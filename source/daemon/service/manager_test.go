package service

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestNewInvalidConfig(t *testing.T) {
	m, err := New(Config{}, WithGOOS("linux"), notInContainer())
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, must not match ErrUnsupported", err)
	}
	if m != nil {
		t.Fatalf("Manager = %v, want nil", m)
	}
}

func TestNewUnsupported(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "plan9"} {
		t.Run(goos, func(t *testing.T) {
			m, err := New(validConfig(goos), WithGOOS(goos), notInContainer())
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, must not match ErrInvalidConfig", err)
			}
			if !strings.Contains(err.Error(), goos) {
				t.Fatalf("err = %q, want it to name %q", err, goos)
			}
			if m != nil {
				t.Fatalf("Manager = %v, want nil", m)
			}
		})
	}
}

func TestNewWithGOOSDrivesValidation(t *testing.T) {
	cfg := validConfig("linux")
	cfg.Binary = "/usr/bin/x"
	cfg.WorkingDir = ""
	cfg.User = ""
	m, err := New(cfg, WithGOOS("windows"), notInContainer())
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	if m != nil {
		t.Fatalf("Manager = %v, want nil", m)
	}
}

func TestNewDefaultsToRuntimeGOOS(t *testing.T) {
	cfg := validConfig(runtime.GOOS)
	_, err := New(cfg, notInContainer())
	_, errExplicit := New(cfg, WithGOOS(runtime.GOOS), notInContainer())
	_, errEmpty := New(cfg, WithGOOS(""), notInContainer())
	if err == nil || err.Error() != errExplicit.Error() || err.Error() != errEmpty.Error() {
		t.Fatalf("default = %v, explicit = %v, empty = %v; want equal", err, errExplicit, errEmpty)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Fatalf("err = %q, want it to name %q", err, runtime.GOOS)
	}
}

const wantInstead = "the container runtime supervises this process; run it as the image's entrypoint and give the container a restart policy that restarts on exit 0 (Docker --restart unless-stopped, Compose restart: unless-stopped, or a Kubernetes Deployment)"

// notInContainer keeps New from probing the host for a container.
func notInContainer() Option {
	return WithContainer(func() bool { return false })
}

func inContainer() Option {
	return WithContainer(func() bool { return true })
}

func testIdentity() spec.Identity {
	return spec.Identity{
		Name:      "demo",
		Owner:     "vipengele",
		Repo:      "demo",
		EnvPrefix: "DEMO",
		DirName:   "demo",
	}
}

// env returns a LookupEnv that sees only vars.
func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func assertContainerError(t *testing.T, m Manager, err error) {
	t.Helper()
	if !errors.Is(err, ErrContainer) {
		t.Fatalf("err = %v, want ErrContainer", err)
	}
	var ce *ContainerError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T, want *ContainerError", err)
	}
	if ce.Instead != wantInstead {
		t.Fatalf("Instead = %q, want %q", ce.Instead, wantInstead)
	}
	if want := "service: running in a container: " + wantInstead; err.Error() != want {
		t.Fatalf("err = %q, want %q", err, want)
	}
	if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, must match neither ErrUnsupported nor ErrInvalidConfig", err)
	}
	if m != nil {
		t.Fatalf("Manager = %v, want nil", m)
	}
}

func TestNewRefusesInAContainerOnEveryGOOS(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "plan9"} {
		t.Run(goos, func(t *testing.T) {
			m, err := New(validConfig(goos), WithGOOS(goos), inContainer())
			assertContainerError(t, m, err)
		})
	}
}

func TestNewRefusesInAContainerBeforeValidatingConfig(t *testing.T) {
	m, err := New(Config{}, WithGOOS("linux"), inContainer())
	assertContainerError(t, m, err)
}

func TestNewInvalidIdentity(t *testing.T) {
	id := testIdentity()
	id.EnvPrefix = "1bad"
	for name, opt := range map[string]Option{"detected": WithLookupEnv(env(nil)), "seam": inContainer()} {
		t.Run(name, func(t *testing.T) {
			m, err := New(validConfig("linux"), WithGOOS("linux"), WithIdentity(id), opt)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
			if errors.Is(err, ErrContainer) {
				t.Fatalf("err = %v, must not match ErrContainer", err)
			}
			if m != nil {
				t.Fatalf("Manager = %v, want nil", m)
			}
		})
	}
}

func TestNewIdentityEnvOverrideRefuses(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			m, err := New(validConfig(goos), WithGOOS(goos), WithIdentity(testIdentity()),
				WithLookupEnv(env(map[string]string{"DEMO_CONTAINER": "1"})))
			assertContainerError(t, m, err)
		})
	}
}

func TestNewIdentityEnvOverrideAllows(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"), WithIdentity(testIdentity()),
		WithLookupEnv(env(map[string]string{"DEMO_CONTAINER": "0", "container": "podman"})))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if m != nil {
		t.Fatalf("Manager = %v, want nil", m)
	}
}

func TestNewWithoutIdentityIgnoresPrefixedOverride(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"),
		WithLookupEnv(env(map[string]string{"DEMO_CONTAINER": "0", "container": "podman"})))
	assertContainerError(t, m, err)
}

func TestNewWithContainerBeatsIdentity(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"), WithIdentity(testIdentity()),
		WithLookupEnv(env(map[string]string{"DEMO_CONTAINER": "1", "container": "docker"})),
		notInContainer())
	if errors.Is(err, ErrContainer) {
		t.Fatalf("err = %v, must not match ErrContainer", err)
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if m != nil {
		t.Fatalf("Manager = %v, want nil", m)
	}
}

func TestNewWithContainerFalseLeavesResults(t *testing.T) {
	_, invalid := New(Config{}, WithGOOS("linux"), notInContainer())
	if !errors.Is(invalid, ErrInvalidConfig) {
		t.Fatalf("invalid config: err = %v, want ErrInvalidConfig", invalid)
	}
	_, unsupported := New(validConfig("darwin"), WithGOOS("darwin"), notInContainer())
	if !errors.Is(unsupported, ErrUnsupported) {
		t.Fatalf("valid config: err = %v, want ErrUnsupported", unsupported)
	}
}

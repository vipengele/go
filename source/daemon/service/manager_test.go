package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

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
	for _, goos := range []string{"windows", "freebsd", "plan9"} {
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

func TestNewLinuxReturnsTheSystemdDriver(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"), notInContainer())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if m == nil {
		t.Fatal("Manager = nil, want the systemd driver")
	}
}

func TestNewDarwinReturnsTheLaunchdDriver(t *testing.T) {
	for name, scope := range map[string]Scope{"system": ScopeSystem, "user": ScopeUser} {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig("darwin")
			cfg.Scope = scope
			if scope == ScopeUser {
				cfg.User = ""
			}
			m, err := New(cfg, WithGOOS("darwin"), WithLaunchdLogs("/var/log/a.out", "/var/log/a.err"), notInContainer())
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			l, ok := m.(*launchd)
			if !ok {
				t.Fatalf("Manager = %T, want *launchd", m)
			}
			if !reflect.DeepEqual(l.cfg, cfg) {
				t.Fatalf("cfg = %+v, want %+v", l.cfg, cfg)
			}
			if want := (launchdLogs{stdout: "/var/log/a.out", stderr: "/var/log/a.err"}); l.logs != want {
				t.Fatalf("logs = %+v, want %+v", l.logs, want)
			}
			if l.euid != os.Geteuid() || l.uid != os.Getuid() {
				t.Fatalf("euid, uid = %d, %d, want %d, %d", l.euid, l.uid, os.Geteuid(), os.Getuid())
			}
			if l.run == nil || l.lookupEnv == nil || l.sleep == nil {
				t.Fatal("run, lookupEnv and sleep must all be set")
			}
			if l.daemonDir != "" {
				t.Fatalf("daemonDir = %q, want empty for /Library/LaunchDaemons", l.daemonDir)
			}
		})
	}
}

func TestNewDarwinKeepsDefaultLaunchdLogs(t *testing.T) {
	m, err := New(validConfig("darwin"), WithGOOS("darwin"), notInContainer())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	l, ok := m.(*launchd)
	if !ok {
		t.Fatalf("Manager = %T, want *launchd", m)
	}
	if l.logs != (launchdLogs{}) {
		t.Fatalf("logs = %+v, want empty so the driver's defaults apply", l.logs)
	}
}

func TestSleepContextWaits(t *testing.T) {
	const d = 20 * time.Millisecond
	start := time.Now()
	if err := sleepContext(context.Background(), d); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed < d {
		t.Fatalf("returned after %v, want at least %v", elapsed, d)
	}
}

func TestSleepContextReturnsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := sleepContext(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Minute {
		t.Fatalf("returned after %v, want it to return on cancellation", elapsed)
	}
}

func TestSleepContextReturnsTheDeadlineError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestSystemctlMapsErrors(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		err   error
		want  error
		argv  []string
	}{
		{"system", ScopeSystem, nil, nil, []string{"systemctl", "start", "x.service"}},
		{"user prefix", ScopeUser, nil, nil, []string{"systemctl", "--user", "start", "x.service"}},
		{"not found", ScopeSystem, fmt.Errorf("run: %w", exec.ErrNotFound), ErrUnsupported, nil},
		{"no bus", ScopeUser, errors.New("exit status 1: Failed to connect to bus: No medium found"), ErrManagerUnreachable, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			s := &systemd{
				cfg: Config{Name: "x", Scope: tc.scope},
				run: func(_ context.Context, argv []string) ([]byte, error) {
					got = argv
					return nil, tc.err
				},
			}
			_, err := s.systemctl(context.Background(), "start", "x.service")
			if tc.want == nil {
				if err != nil || !slices.Equal(got, tc.argv) {
					t.Fatalf("err = %v, argv = %v, want nil, %v", err, got, tc.argv)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
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
	if fmt.Sprint(err) != fmt.Sprint(errExplicit) || fmt.Sprint(err) != fmt.Sprint(errEmpty) {
		t.Fatalf("default = %v, explicit = %v, empty = %v; want equal", err, errExplicit, errEmpty)
	}
	if err != nil && !strings.Contains(err.Error(), runtime.GOOS) {
		t.Fatalf("err = %q, want it to name %q", err, runtime.GOOS)
	}
}

func TestSystemdUnitPath(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		env   map[string]string
		want  string
		fails bool
	}{
		{"system", ScopeSystem, nil, "/etc/systemd/system/x.service", false},
		{"xdg", ScopeUser, map[string]string{"XDG_CONFIG_HOME": "/c", "HOME": "/h"}, "/c/systemd/user/x.service", false},
		{"home", ScopeUser, map[string]string{"HOME": "/h"}, "/h/.config/systemd/user/x.service", false},
		{"neither", ScopeUser, nil, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &systemd{cfg: Config{Name: "x", Scope: tc.scope}, lookupEnv: env(tc.env)}
			got, err := s.unitPath()
			if tc.fails != (err != nil) || got != tc.want {
				t.Fatalf("unitPath = %q, %v; want %q, fails=%v", got, err, tc.want, tc.fails)
			}
		})
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

// assertNotRefused fails unless New returned a driver: the container check let
// it through.
func assertNotRefused(t *testing.T, m Manager, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if m == nil {
		t.Fatal("Manager = nil, want a driver")
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

func TestNewRefusesInAContainerBeforeValidatingHardening(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"),
		WithSystemdHardening(Hardening{ProtectSystem: "yes"}), inContainer())
	assertContainerError(t, m, err)
}

func TestNewRefusesInAContainerBeforeValidatingNotify(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"), WithSystemdNotify(-time.Second), inContainer())
	assertContainerError(t, m, err)
}

func TestNewValidatesSystemdNotifyOnEveryGOOS(t *testing.T) {
	tests := []struct {
		name     string
		watchdog time.Duration
		invalid  bool
	}{
		{"zero", 0, false},
		{"whole seconds", 30 * time.Second, false},
		{"whole milliseconds", 1500 * time.Millisecond, false},
		{"negative", -time.Second, true},
		{"sub-millisecond fraction", time.Second + time.Microsecond, true},
		{"below a millisecond", time.Nanosecond, true},
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, tc := range tests {
			t.Run(goos+"/"+tc.name, func(t *testing.T) {
				m, err := New(validConfig(goos), WithGOOS(goos), WithSystemdNotify(tc.watchdog), notInContainer())
				if tc.invalid {
					if !errors.Is(err, ErrInvalidConfig) {
						t.Fatalf("err = %v, want ErrInvalidConfig", err)
					}
					if !strings.Contains(err.Error(), "WithSystemdNotify") {
						t.Fatalf("err = %q, want it to name WithSystemdNotify", err)
					}
					if m != nil {
						t.Fatalf("Manager = %v, want nil", m)
					}
					return
				}
				if errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("err = %v, want no ErrInvalidConfig", err)
				}
				if goos != "windows" {
					assertNotRefused(t, m, err)
				}
			})
		}
	}
}

func TestNewValidatesSystemdNotifyAfterHardening(t *testing.T) {
	_, err := New(validConfig("linux"), WithGOOS("linux"),
		WithSystemdHardening(Hardening{ProtectSystem: "yes"}), WithSystemdNotify(-time.Second), notInContainer())
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "Hardening.ProtectSystem") {
		t.Fatalf("err = %v, want the Hardening violation", err)
	}
	if strings.Contains(err.Error(), "WithSystemdNotify") {
		t.Fatalf("err = %v, want only the Hardening violation", err)
	}
}

func TestNewPassesSystemdNotifyToTheDriver(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"), WithSystemdNotify(30*time.Second), notInContainer())
	assertNotRefused(t, m, err)
	s, ok := m.(*systemd)
	if !ok {
		t.Fatalf("Manager = %T, want *systemd", m)
	}
	if want := (notify{enabled: true, watchdog: 30 * time.Second}); s.notify != want {
		t.Fatalf("notify = %+v, want %+v", s.notify, want)
	}
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
	assertNotRefused(t, m, err)
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
	assertNotRefused(t, m, err)
}

func TestNewWithContainerFalseLeavesResults(t *testing.T) {
	_, invalid := New(Config{}, WithGOOS("linux"), notInContainer())
	if !errors.Is(invalid, ErrInvalidConfig) {
		t.Fatalf("invalid config: err = %v, want ErrInvalidConfig", invalid)
	}
	_, unsupported := New(validConfig("freebsd"), WithGOOS("freebsd"), notInContainer())
	if !errors.Is(unsupported, ErrUnsupported) {
		t.Fatalf("valid config: err = %v, want ErrUnsupported", unsupported)
	}
}

func TestNewValidatesLaunchdLogsOnEveryGOOS(t *testing.T) {
	tests := []struct {
		name           string
		stdout, stderr string
		invalid        bool
	}{
		{"both empty", "", "", false},
		{"absolute", "/var/log/tool.out", "/var/log/tool.err", false},
		{"stdout only", "/var/log/tool.out", "", false},
		{"stderr only", "", "/var/log/tool.err", false},
		{"relative stdout", "tool.out", "", true},
		{"relative stderr", "", "logs/tool.err", true},
		{"windows drive path", `C:\logs\tool.out`, "", true},
		{"control character in stdout", "/var/log/a\nb", "", true},
		{"control character in stderr", "", "/var/log/a\x7fb", true},
		{"invalid UTF-8", "/var/log/\xff", "", true},
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, tc := range tests {
			t.Run(goos+"/"+tc.name, func(t *testing.T) {
				m, err := New(validConfig(goos), WithGOOS(goos), WithLaunchdLogs(tc.stdout, tc.stderr), notInContainer())
				if tc.invalid {
					if !errors.Is(err, ErrInvalidConfig) {
						t.Fatalf("err = %v, want ErrInvalidConfig", err)
					}
					if !strings.Contains(err.Error(), "WithLaunchdLogs") {
						t.Fatalf("err = %q, want it to name WithLaunchdLogs", err)
					}
					if m != nil {
						t.Fatalf("Manager = %v, want nil", m)
					}
					return
				}
				if errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("err = %v, want no ErrInvalidConfig", err)
				}
				if goos != "windows" {
					assertNotRefused(t, m, err)
				}
			})
		}
	}
}

func TestNewValidatesLaunchdLogsBeforeTheGOOSSwitch(t *testing.T) {
	_, err := New(validConfig("freebsd"), WithGOOS("freebsd"), WithLaunchdLogs("tool.out", ""), notInContainer())
	if !errors.Is(err, ErrInvalidConfig) || errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrInvalidConfig and not ErrUnsupported", err)
	}
}

func TestNewValidatesLaunchdLogsAfterNotify(t *testing.T) {
	_, err := New(validConfig("linux"), WithGOOS("linux"),
		WithSystemdNotify(-time.Second), WithLaunchdLogs("tool.out", ""), notInContainer())
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "WithSystemdNotify") {
		t.Fatalf("err = %v, want the WithSystemdNotify violation", err)
	}
	if strings.Contains(err.Error(), "WithLaunchdLogs") {
		t.Fatalf("err = %v, want only the WithSystemdNotify violation", err)
	}
}

func TestNewRefusesInAContainerBeforeValidatingLaunchdLogs(t *testing.T) {
	m, err := New(validConfig("darwin"), WithGOOS("darwin"), WithLaunchdLogs("tool.out", ""), inContainer())
	assertContainerError(t, m, err)
}

func TestNewStoresLaunchdLogs(t *testing.T) {
	var o options
	WithLaunchdLogs("/var/log/a", "/var/log/b")(&o)
	if want := (launchdLogs{stdout: "/var/log/a", stderr: "/var/log/b"}); o.launchd != want {
		t.Fatalf("launchd = %+v, want %+v", o.launchd, want)
	}
}

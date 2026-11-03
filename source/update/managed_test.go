package update

import (
	"errors"
	"os"
	"testing"
)

const (
	brewInstead    = "run `brew upgrade atk`"
	wantNixInstead = "update it through your Nix configuration (`nixos-rebuild switch`, `home-manager switch` or `nix profile upgrade atk`)"
	imageStep      = "pull a newer image of the container"
)

// managedConfig is a Config whose executable is path, whose symlinks resolve through eval, and
// whose container check answers inContainer, so no result depends on the host.
func managedConfig(path string, eval func(string) (string, error), inContainer bool) Config {
	c := validConfig(os.TempDir())
	c.Executable = func() (string, error) { return path, nil }
	c.EvalSymlinks = eval
	c.InContainer = func() bool { return inContainer }

	return c
}

func identityEval(p string) (string, error) { return p, nil }

func TestCheckManagedBuiltInDetectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		wantManager string
		wantInstead string
	}{
		{name: "homebrew on apple silicon", path: "/opt/homebrew/bin/atk", wantManager: "Homebrew", wantInstead: brewInstead},
		{name: "homebrew exact prefix", path: "/opt/homebrew", wantManager: "Homebrew", wantInstead: brewInstead},
		{name: "homebrew prefix and slash", path: "/opt/homebrew/", wantManager: "Homebrew", wantInstead: brewInstead},
		{name: "homebrew cellar", path: "/usr/local/Cellar/atk/1.0/bin/atk", wantManager: "Homebrew", wantInstead: brewInstead},
		{name: "homebrew caskroom", path: "/usr/local/Caskroom/atk/1.0/atk", wantManager: "Homebrew", wantInstead: brewInstead},
		{name: "linuxbrew", path: "/home/linuxbrew/.linuxbrew/bin/atk", wantManager: "Homebrew", wantInstead: brewInstead},
		{name: "nix store", path: "/nix/store/abc-atk/bin/atk", wantManager: "Nix", wantInstead: wantNixInstead},
		{name: "nix exact prefix", path: "/nix/store", wantManager: "Nix", wantInstead: wantNixInstead},
		{name: "nix prefix and slash", path: "/nix/store/", wantManager: "Nix", wantInstead: wantNixInstead},
		{name: "nix profile is not the store", path: "/nix/var/nix/profiles/atk"},
		{name: "prefix with a suffix", path: "/opt/homebrewX/bin/atk"},
		{name: "cellar with a suffix", path: "/usr/local/CellarX/atk"},
		{name: "caskroom with a suffix", path: "/usr/local/CaskroomX/atk"},
		{name: "linuxbrew with a suffix", path: "/home/linuxbrew/.linuxbrewX/bin/atk"},
		{name: "nix store with a suffix", path: "/nix/storeX/atk"},
		{name: "case differs", path: "/opt/Homebrew/bin/atk"},
		{name: "nix case differs", path: "/Nix/store/atk"},
		{name: "unmanaged", path: "/usr/local/bin/atk"},
		{name: "relative path", path: "opt/homebrew/bin/atk"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := managedConfig(tc.path, identityEval, false).CheckManaged()

			if tc.wantManager == "" {
				if err != nil {
					t.Fatalf("CheckManaged() = %v, want nil", err)
				}

				return
			}

			var managed *ManagedInstallError
			if !errors.As(err, &managed) {
				t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
			}

			want := ManagedInstallError{Path: tc.path, Resolved: tc.path, Manager: tc.wantManager, Instead: tc.wantInstead}
			if *managed != want {
				t.Errorf("error = %+v, want %+v", *managed, want)
			}
		})
	}
}

func TestCheckManagedUsesBinaryName(t *testing.T) {
	t.Parallel()

	c := managedConfig("/opt/homebrew/bin/other", identityEval, false)
	c.BinaryName = "other"

	var managed *ManagedInstallError
	if err := c.CheckManaged(); !errors.As(err, &managed) {
		t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
	}

	if want := "run `brew upgrade other`"; managed.Instead != want {
		t.Errorf("Instead = %q, want %q", managed.Instead, want)
	}
}

func TestCheckManagedResolvesSymlinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		resolved     string
		wantManager  string
		wantResolved string
	}{
		{name: "into the cellar", path: "/usr/local/bin/atk", resolved: "/usr/local/Cellar/atk/1.0/bin/atk", wantManager: "Homebrew", wantResolved: "/usr/local/Cellar/atk/1.0/bin/atk"},
		{name: "into the nix store", path: "/home/u/.nix-profile/bin/atk", resolved: "/nix/store/abc-atk/bin/atk", wantManager: "Nix", wantResolved: "/nix/store/abc-atk/bin/atk"},
		{name: "out of a managed prefix", path: "/opt/homebrew/bin/atk", resolved: "/home/u/bin/atk"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			eval := func(p string) (string, error) {
				if p != tc.path {
					t.Errorf("EvalSymlinks(%q), want %q", p, tc.path)
				}

				return tc.resolved, nil
			}

			err := managedConfig(tc.path, eval, false).CheckManaged()

			if tc.wantManager == "" {
				if err != nil {
					t.Fatalf("CheckManaged() = %v, want nil", err)
				}

				return
			}

			var managed *ManagedInstallError
			if !errors.As(err, &managed) {
				t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
			}

			if managed.Path != tc.path || managed.Resolved != tc.wantResolved || managed.Manager != tc.wantManager {
				t.Errorf("error = %+v", *managed)
			}
		})
	}
}

func TestCheckManagedMatchesTheUnresolvedPathWhenResolutionFails(t *testing.T) {
	t.Parallel()

	eval := func(string) (string, error) { return "/nix/store/never-used", errors.New("boom") }

	var managed *ManagedInstallError
	err := managedConfig("/opt/homebrew/bin/atk", eval, false).CheckManaged()
	if !errors.As(err, &managed) {
		t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
	}

	if managed.Manager != "Homebrew" || managed.Path != "/opt/homebrew/bin/atk" || managed.Resolved != managed.Path {
		t.Errorf("error = %+v", *managed)
	}

	if err := managedConfig("/usr/local/bin/atk", eval, false).CheckManaged(); err != nil {
		t.Errorf("CheckManaged() = %v, want nil", err)
	}
}

func TestCheckManagedContainer(t *testing.T) {
	t.Parallel()

	var managed *ManagedInstallError
	err := managedConfig("/usr/local/bin/atk", identityEval, true).CheckManaged()
	if !errors.As(err, &managed) {
		t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
	}

	want := ManagedInstallError{Path: "/usr/local/bin/atk", Resolved: "/usr/local/bin/atk", Manager: "a container", Instead: imageStep}
	if *managed != want {
		t.Errorf("error = %+v, want %+v", *managed, want)
	}
}

func TestCheckManagedDetectorOrder(t *testing.T) {
	t.Parallel()

	extra := func(name string, ok bool, calls *[]string) Detector {
		return func(path string) (Install, bool) {
			*calls = append(*calls, name+":"+path)

			return Install{Manager: name, Instead: "use " + name}, ok
		}
	}

	t.Run("first extra match wins", func(t *testing.T) {
		t.Parallel()

		var calls []string
		c := managedConfig("/usr/local/bin/atk", identityEval, false)
		c.Detectors = []Detector{extra("one", false, &calls), extra("two", true, &calls), extra("three", true, &calls)}

		var managed *ManagedInstallError
		if err := c.CheckManaged(); !errors.As(err, &managed) {
			t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
		}

		if managed.Manager != "two" || managed.Instead != "use two" {
			t.Errorf("error = %+v", *managed)
		}

		if len(calls) != 2 || calls[0] != "one:/usr/local/bin/atk" || calls[1] != "two:/usr/local/bin/atk" {
			t.Errorf("calls = %v", calls)
		}
	})

	t.Run("extra sees the resolved path", func(t *testing.T) {
		t.Parallel()

		var calls []string
		c := managedConfig("/a/link", func(string) (string, error) { return "/b/real", nil }, false)
		c.Detectors = []Detector{extra("one", false, &calls)}

		if err := c.CheckManaged(); err != nil {
			t.Fatalf("CheckManaged() = %v, want nil", err)
		}

		if len(calls) != 1 || calls[0] != "one:/b/real" {
			t.Errorf("calls = %v", calls)
		}
	})

	builtIns := []struct {
		name        string
		path        string
		inContainer bool
		wantManager string
	}{
		{name: "nix", path: "/nix/store/x/atk", wantManager: "Nix"},
		{name: "homebrew", path: "/opt/homebrew/bin/atk", wantManager: "Homebrew"},
		{name: "container", path: "/usr/local/bin/atk", inContainer: true, wantManager: "a container"},
	}

	for _, tc := range builtIns {
		t.Run("built-in "+tc.name+" wins over an extra", func(t *testing.T) {
			t.Parallel()

			var calls []string
			c := managedConfig(tc.path, identityEval, tc.inContainer)
			c.Detectors = []Detector{extra("one", true, &calls)}

			var managed *ManagedInstallError
			if err := c.CheckManaged(); !errors.As(err, &managed) {
				t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
			}

			if managed.Manager != tc.wantManager {
				t.Errorf("Manager = %q, want %q", managed.Manager, tc.wantManager)
			}

			if len(calls) != 0 {
				t.Errorf("extra detector ran: %v", calls)
			}
		})
	}

	t.Run("nix wins over homebrew and the container", func(t *testing.T) {
		t.Parallel()

		var managed *ManagedInstallError
		err := managedConfig("/nix/store/x/atk", identityEval, true).CheckManaged()
		if !errors.As(err, &managed) || managed.Manager != "Nix" {
			t.Fatalf("CheckManaged() = %v, want Nix", err)
		}
	})

	t.Run("homebrew wins over the container", func(t *testing.T) {
		t.Parallel()

		var managed *ManagedInstallError
		err := managedConfig("/opt/homebrew/bin/atk", identityEval, true).CheckManaged()
		if !errors.As(err, &managed) || managed.Manager != "Homebrew" {
			t.Fatalf("CheckManaged() = %v, want Homebrew", err)
		}
	})
}

func TestCheckManagedReportsAnExecutableError(t *testing.T) {
	t.Parallel()

	boom := errors.New("no executable")
	c := managedConfig("", identityEval, true)
	c.Executable = func() (string, error) { return "/opt/homebrew/bin/atk", boom }

	err := c.CheckManaged()
	if !errors.Is(err, boom) {
		t.Fatalf("CheckManaged() = %v, want it to wrap %v", err, boom)
	}

	if want := "update: locate executable: no executable"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestCheckManagedRejectsAnInvalidConfig(t *testing.T) {
	t.Parallel()

	c := managedConfig("/opt/homebrew/bin/atk", identityEval, true)
	c.Detectors = []Detector{nil}

	if err := c.CheckManaged(); err == nil {
		t.Fatal("CheckManaged() = nil, want a validation error")
	}
}

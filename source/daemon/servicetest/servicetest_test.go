package servicetest

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"go.vipengele.org/daemon/service"
)

// TestSupervisor proves the sample daemon under systemd in a podman container,
// as a system service, a user service and a system service of Type=notify. It
// runs only with SERVICETEST=1. The driver is built once and shared by every
// target, so the three runs do not each rebuild it.
func TestSupervisor(t *testing.T) {
	SkipUnlessEnabled(t)

	binary := filepath.Join(t.TempDir(), "sampled")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./testdata/sampled") // #nosec G204 -- the program is the constant go; its arguments are the constant build verb and package and an output path under the test's temporary directory; no shell is involved
	build.Env = append(build.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building testdata/sampled: %v\n%s", err, out)
	}
	driver := filepath.Join(t.TempDir(), "servicetest-driver")
	if out, err := goBuildDriver(t.Context(), driver); err != nil {
		t.Fatalf("building the driver: %v\n%s", err, out)
	}

	targets := []struct {
		name   string
		target Target
	}{
		{"system", Target{Config: config("sampled", service.ScopeSystem)}},
		{"user", Target{Config: config("sampled", service.ScopeUser)}},
		{"system_notify", Target{
			Config: config("sampled-notify", service.ScopeSystem),
			Notify: &Notify{},
		}},
	}
	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			tc.target.Binary = binary
			tc.target.Driver = driver
			Run(t, tc.target)
		})
	}
}

// config is the sample daemon installed under name in scope.
func config(name string, scope service.Scope) service.Config {
	return service.Config{
		Name:        name,
		Description: "servicetest sample daemon",
		Binary:      "/usr/local/bin/sampled",
		Scope:       scope,
	}
}

package servicetest

import (
	"context"
	"os"
	"testing"
	"time"

	"go.vipengele.org/daemon/service"
)

const (
	// enableEnv names the variable that must be "1" for any container to run.
	enableEnv = "SERVICETEST"
	// DefaultTimeout bounds the image build and each scope's run when
	// Target.Timeout is zero.
	DefaultTimeout = 10 * time.Minute
)

// Target is what Run proves under systemd.
type Target struct {
	// BaseImage replaces the Debian base image the container is built from.
	// It must be a Debian-based image that installs systemd with apt-get.
	// Empty means the base pinned by digest in the embedded Containerfile.
	BaseImage string
	// Scopes are the service scopes to prove, each in its own container.
	// Empty means service.ScopeSystem and service.ScopeUser.
	Scopes []service.Scope
	// Timeout bounds the image build and, separately, each scope's run. Zero
	// means DefaultTimeout.
	Timeout time.Duration
}

// SkipUnlessEnabled skips tb unless the environment variable SERVICETEST is
// "1".
func SkipUnlessEnabled(tb testing.TB) {
	tb.Helper()
	skipUnless(tb, os.LookupEnv)
}

// skipUnless skips tb unless lookup reports SERVICETEST as "1".
func skipUnless(tb testing.TB, lookup func(string) (string, bool)) {
	tb.Helper()
	if v, _ := lookup(enableEnv); v != "1" {
		tb.Skip("servicetest: set " + enableEnv + "=1 to run the service in a podman container under systemd")
	}
}

// Run proves target in one podman container per scope, with systemd as PID
// 1, as a subtest of t named after the scope. It skips t unless SERVICETEST
// is "1", and fails it when podman is not usable or a container does not
// boot. Every container is removed when its subtest ends.
func Run(t *testing.T, target Target) {
	t.Helper()
	SkipUnlessEnabled(t)
	runScopes(t, newHarness(t, os.LookupEnv), target, nil)
}

// plan holds what a run derives from a Target before any container starts.
type plan struct {
	image   string
	scopes  []service.Scope
	timeout time.Duration
}

// prepare checks target's scopes, builds or reuses the image, and returns the
// plan; it fails tb when either does not succeed.
func prepare(tb testing.TB, h harness, target Target) plan {
	tb.Helper()
	p := plan{scopes: target.Scopes, timeout: target.Timeout}
	if len(p.scopes) == 0 {
		p.scopes = []service.Scope{service.ScopeSystem, service.ScopeUser}
	}
	for _, s := range p.scopes {
		if _, err := scopeName(s); err != nil {
			tb.Fatal(err)
		}
	}
	if p.timeout <= 0 {
		p.timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(tb.Context(), p.timeout)
	defer cancel()
	image, err := h.ensureImage(ctx, target.BaseImage, tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	p.image = image
	return p
}

// runScopes boots a container for each scope of target in a subtest of t and
// calls body, when non-nil, with the booted container.
func runScopes(t *testing.T, h harness, target Target, body func(*testing.T, *container)) {
	t.Helper()
	p := prepare(t, h, target)
	for _, scope := range p.scopes {
		name, _ := scopeName(scope)
		t.Run(name, func(t *testing.T) {
			c := boot(t, h, p.image, scope, p.timeout)
			if body != nil {
				body(t, c)
			}
		})
	}
}

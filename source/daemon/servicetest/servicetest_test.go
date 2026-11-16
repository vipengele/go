package servicetest

import (
	"os"
	"strings"
	"testing"

	"go.vipengele.org/daemon/service"
)

// TestSupervisor boots a container for each scope under real podman and
// checks that its system manager is up and, for the user scope, that the
// lingering user manager is active. It runs only with SERVICETEST=1.
func TestSupervisor(t *testing.T) {
	SkipUnlessEnabled(t)
	h := newHarness(t, os.LookupEnv)
	for _, scope := range []service.Scope{service.ScopeSystem, service.ScopeUser} {
		runScope(t, h, scope, "", func(t *testing.T, c *container) {
			ctx := t.Context()
			out, _ := c.exec(ctx, execOptions{}, "systemctl", "is-system-running")
			if s := strings.TrimSpace(string(out)); s != "running" && s != "degraded" {
				t.Errorf("system manager state = %q, want running or degraded", s)
			}
			if c.scope != service.ScopeUser {
				return
			}
			unit := c.userManagerUnit()
			out, err := c.exec(ctx, execOptions{}, "systemctl", "is-active", unit)
			if s := strings.TrimSpace(string(out)); s != "active" {
				t.Errorf("%s = %q (%v), want active", unit, s, err)
				c.logDiagnostics(ctx, t, unit, false)
			}
		})
	}
}

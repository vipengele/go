//go:build !windows

package daemoncmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"go.vipengele.org/daemon/service"
)

func TestUpdateRestartsAServiceWhoseBinaryLinksToTheInstalledOne(t *testing.T) {
	f := newUpdateFixture(t)
	link := filepath.Join(t.TempDir(), "service-"+updateName)
	if err := os.Symlink(f.exe, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	mgr := newSequence(nil, service.StateRunning)
	cfg := f.config(mgr.managerFunc())
	cfg.ServiceBinary = link

	r := runUpdate(t, cfg, f.clock)
	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	assertCalls(t, mgr.fakeManager, "manager", "Status", "Restart", "Status")
	f.assertInstalled(t)
}

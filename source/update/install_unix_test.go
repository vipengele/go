//go:build !windows

package update

import (
	"os"
	"testing"
)

// TestInstallCopiesModeWithoutSetuid runs only where files carry Unix permission bits, which
// Windows lacks.
func TestInstallCopiesModeWithoutSetuid(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)

	if err := os.Chmod(fx.exe, 0o751|os.ModeSetuid|os.ModeSetgid|os.ModeSticky); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	if old, err := os.Stat(fx.exe); err != nil || old.Mode()&os.ModeSetuid == 0 {
		t.Skipf("the filesystem does not keep setuid: %v", err)
	}

	inst, err := fx.config.Install(fx.staged)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	info, err := os.Stat(inst.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if got, want := info.Mode(), os.FileMode(0o751); got != want {
		t.Errorf("mode = %v, want %v", got, want)
	}
}

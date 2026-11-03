//go:build !windows

package update

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// TestCheckWritableReadOnlyDir runs only where directories carry Unix permission bits, which
// Windows lacks, and only as a user other than root, whom permission bits do not restrict.
func TestCheckWritableReadOnlyDir(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}

	dir := resolvedDir(t, t.TempDir())
	exe := writeExecutable(t, dir, "atk")

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := executableConfig(t, exe).CheckWritable()

	var perr *PermissionError
	if !errors.As(err, &perr) {
		t.Fatalf("CheckWritable error = %v, want a *PermissionError", err)
	}

	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("CheckWritable error = %v, want one matching fs.ErrPermission", err)
	}

	if perr.Op != "create" || perr.Path != dir {
		t.Errorf("Op, Path = %q, %q, want %q, %q", perr.Op, perr.Path, "create", dir)
	}

	if perr.Hint == "" {
		t.Error("Hint is empty")
	}
}

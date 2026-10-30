//go:build !windows

package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckManagedResolvesARealSymlink(t *testing.T) {
	t.Parallel()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "target", "atk")
	link := filepath.Join(dir, "link")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(target, nil, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	c := validConfig(dir)
	c.Executable = func() (string, error) { return link, nil }
	c.InContainer = func() bool { return false }
	c.Detectors = []Detector{func(path string) (Install, bool) {
		return Install{Manager: "Test", Instead: "x"}, path == target
	}}

	var managed *ManagedInstallError
	if err := c.CheckManaged(); !errors.As(err, &managed) {
		t.Fatalf("CheckManaged() = %v, want *ManagedInstallError", err)
	}

	if managed.Path != link || managed.Resolved != target {
		t.Errorf("Path = %q, Resolved = %q, want %q and %q", managed.Path, managed.Resolved, link, target)
	}
}

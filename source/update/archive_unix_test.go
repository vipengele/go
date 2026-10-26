//go:build !windows

package update

import (
	"os"
	"testing"
)

// TestCreateArchiveMode runs only where files carry Unix permission bits, which Windows lacks.
func TestCreateArchiveMode(t *testing.T) {
	t.Parallel()

	a, _ := newFinishedArchive(t)

	info, err := os.Stat(a.Name())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %v, want 0600", got)
	}
}

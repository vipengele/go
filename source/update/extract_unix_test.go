//go:build !windows

package update

import (
	"context"
	"os"
	"testing"
)

// TestExtractMode runs only where files carry Unix permission bits, which Windows lacks.
func TestExtractMode(t *testing.T) {
	t.Parallel()

	a, _ := newExtractArchive(t, []byte("bin"))

	s, err := rawConfig(t).Extract(context.Background(), a, t.TempDir())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	keepAtCleanup(t, s)

	info, err := os.Stat(s.Name())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("mode = %v, want 0700", got)
	}
}

//go:build unix

package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestFileResponseCacheModes runs only where files carry Unix permission bits.
func TestFileResponseCacheModes(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "state")
	c := &fileCache{dir: dir}

	if err := c.Put(context.Background(), "https://a.test", CachedResponse{ETag: `"a"`}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	for path, want := range map[string]os.FileMode{dir: 0o700, c.path(): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("mode of %s = %v, want %v", path, got, want)
		}
	}
}

//go:build unix

package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestFileStoreSaveModes runs only where files carry Unix permission bits.
func TestFileStoreSaveModes(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "state")
	store := &fileStore{dir: dir}

	if err := store.Save(context.Background(), State{LatestVersion: "v1.0.0"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for path, want := range map[string]os.FileMode{dir: 0o700, store.path(): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("mode of %s = %v, want %v", path, got, want)
		}
	}
}

func TestFileStoreLoadUnreadableFile(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their mode")
	}

	store := &fileStore{dir: t.TempDir()}
	if err := os.WriteFile(store.path(), []byte(`{}`), 0o000); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := store.Load(context.Background()); err == nil {
		t.Error("Load = nil error, want an error")
	}
}

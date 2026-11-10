package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"go.vipengele.org/cli/state"
)

// TestWriteFileOnWindows writes through the real write path on a Windows filesystem, where Sync is
// FlushFileBuffers and needs a handle with write access, and the directory is not synced. A mode
// without owner write sets the read-only attribute, which Stat reports as a mode without 0o200.
func TestWriteFileOnWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     os.FileMode
		writable bool
	}{
		{name: "0600", mode: 0o600, writable: true},
		{name: "0400", mode: 0o400, writable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")

			if err := state.WriteFile(path, []byte("first"), 0o600); err != nil {
				t.Fatalf("WriteFile creating %s: %v", path, err)
			}

			if err := state.WriteFile(path, []byte("second"), tt.mode); err != nil {
				t.Fatalf("WriteFile replacing %s: %v", path, err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if string(got) != "second" {
				t.Errorf("content of %s = %q, want %q", path, got, "second")
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}

			if writable := info.Mode().Perm()&0o200 != 0; writable != tt.writable {
				t.Errorf("mode of %s = %o, want owner write %t", path, info.Mode().Perm(), tt.writable)
			}

			assertEntries(t, dir, "state.json")
		})
	}
}

func TestSaveJSONReplacesOnWindows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".update-state.json")
	writeAt(t, path, "old", 0o644)

	if err := state.SaveJSON(path, map[string]int{"count": 1}); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	got, status, err := state.LoadJSON[map[string]int](path)
	if status != state.Loaded || err != nil || got["count"] != 1 {
		t.Fatalf("LoadJSON = %v, %v, %v; want map[count:1], loaded, <nil>", got, status, err)
	}

	assertEntries(t, dir, ".update-state.json")
}

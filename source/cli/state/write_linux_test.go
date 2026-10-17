package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"go.vipengele.org/cli/state"
)

// TestReplaceFailsWhenTempCannotBeChmodded names a file of /proc as the temporary file: it opens,
// and the proc filesystem refuses every change of mode, whoever runs the test.
func TestReplaceFailsWhenTempCannotBeChmodded(t *testing.T) {
	t.Parallel()

	const tmp = "/proc/self/status"
	if _, err := os.Stat(tmp); err != nil {
		t.Skipf("no %s: %v", tmp, err)
	}

	dir := t.TempDir()
	final := filepath.Join(dir, "config.toml")
	writeAt(t, final, "old", 0o644)

	err := state.Replace(tmp, final, 0o600)
	if err == nil {
		t.Fatal("Replace: want an error, got nil")
	}

	if want := "state: chmod " + tmp + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q does not start with %q", err, want)
	}

	assertFile(t, final, "old", 0o644)
	assertEntries(t, dir, "config.toml")
}

// TestReplaceFailsWhenTempCannotBeSynced uses a named pipe as the temporary file: it opens and
// takes a mode, and fsync of a pipe fails with EINVAL.
func TestReplaceFailsWhenTempCannotBeSynced(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmp := filepath.Join(dir, "config.toml.tmp")
	final := filepath.Join(dir, "config.toml")
	writeAt(t, final, "old", 0o644)

	if err := syscall.Mkfifo(tmp, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	// Opening a pipe for reading blocks until it has a writer; a descriptor open for reading and
	// writing is one, so Replace's open returns at once.
	holder, err := os.OpenFile(tmp, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = holder.Close() })

	err = state.Replace(tmp, final, 0o600)
	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("Replace = %v, want an error matching EINVAL", err)
	}

	if want := "state: sync " + tmp + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q does not start with %q", err, want)
	}

	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp %s remains after a failed Replace (stat error %v)", tmp, err)
	}

	assertFile(t, final, "old", 0o644)
	assertEntries(t, dir, "config.toml")
}

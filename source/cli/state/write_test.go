package state_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.vipengele.org/cli/state"
)

// writeAt writes data to path and sets its mode explicitly, so the result does not depend on the
// process umask.
func writeAt(t *testing.T, path string, data string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string, mode os.FileMode) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Errorf("content of %s = %q, want %q", path, got, want)
	}

	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != mode {
		t.Errorf("mode of %s = %o, want %o", path, perm, mode)
	}
}

func assertEntries(t *testing.T, dir string, want ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("entries of %s = %q, want %q", dir, got, want)
	}
}

func TestReplaceMovesTempOverFinalAtMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing bool
	}{
		{name: "final absent", existing: false},
		{name: "final present at 0644", existing: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			tmp := filepath.Join(dir, "config.toml.tmp")
			final := filepath.Join(dir, "config.toml")

			if tt.existing {
				writeAt(t, final, "old", 0o644)
			}

			writeAt(t, tmp, "new", 0o644)

			if err := state.Replace(tmp, final, 0o600); err != nil {
				t.Fatalf("Replace: %v", err)
			}

			assertFile(t, final, "new", 0o600)
			assertEntries(t, dir, "config.toml")
		})
	}
}

func TestReplaceUsesTheInodeTempNamesAtCallTime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmp := filepath.Join(dir, "config.toml.tmp")
	final := filepath.Join(dir, "config.toml")

	writeAt(t, final, "old", 0o644)
	writeAt(t, tmp, "first", 0o600)

	// An editor that saves by rename writes a new file and moves it over the one it opened,
	// leaving tmp naming a different inode at the umask default.
	saved := filepath.Join(dir, "config.toml.tmp~")
	writeAt(t, saved, "edited", 0o644)

	if err := os.Rename(saved, tmp); err != nil {
		t.Fatal(err)
	}

	if err := state.Replace(tmp, final, 0o600); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	assertFile(t, final, "edited", 0o600)
	assertEntries(t, dir, "config.toml")
}

func TestReplaceFailureRemovesTemp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, tmp, final string)
		want  []string
	}{
		{
			name: "temp missing",
			setup: func(t *testing.T, _, final string) {
				t.Helper()
				writeAt(t, final, "old", 0o644)
			},
			want: []string{"config.toml"},
		},
		{
			name: "final is a non-empty directory",
			setup: func(t *testing.T, tmp, final string) {
				t.Helper()
				writeAt(t, tmp, "new", 0o644)

				if err := os.Mkdir(final, 0o755); err != nil {
					t.Fatal(err)
				}

				writeAt(t, filepath.Join(final, "inside"), "x", 0o644)
			},
			want: []string{"config.toml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			tmp := filepath.Join(dir, "config.toml.tmp")
			final := filepath.Join(dir, "config.toml")

			tt.setup(t, tmp, final)

			if err := state.Replace(tmp, final, 0o600); err == nil {
				t.Fatal("Replace: want an error, got nil")
			}

			if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
				t.Errorf("temp %s remains after a failed Replace (stat error %v)", tmp, err)
			}

			assertEntries(t, dir, tt.want...)
		})
	}
}

func TestWriteFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing bool
	}{
		{name: "creates the file", existing: false},
		{name: "replaces an existing 0644 file", existing: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")

			if tt.existing {
				writeAt(t, path, "old", 0o644)
			}

			if err := state.WriteFile(path, []byte("key = 1\n"), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			assertFile(t, path, "key = 1\n", 0o600)
			assertEntries(t, dir, "config.toml")
		})
	}
}

func TestWriteFileFailureLeavesNoTemp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	writeAt(t, filepath.Join(path, "inside"), "x", 0o644)

	if err := state.WriteFile(path, []byte("new"), 0o600); err == nil {
		t.Fatal("WriteFile: want an error, got nil")
	}

	assertEntries(t, dir, "config.toml")
}

// TestWriteFileRelativePath runs in the process's working directory, which t.Chdir changes, and
// sets the system temporary directory, which os.CreateTemp falls back to when given no directory,
// to one that does not exist. A bare file name therefore succeeds only when the temporary file is
// created beside it, in ".". It is not parallel because both settings are process-wide.
func TestWriteFileRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TMPDIR", filepath.Join(dir, "missing"))

	if err := state.WriteFile("config.toml", []byte("key = 1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	assertFile(t, filepath.Join(dir, "config.toml"), "key = 1\n", 0o600)
	assertEntries(t, dir, "config.toml")
}

func TestWriteFileInAMissingDirectoryFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "config.toml")

	err := state.WriteFile(path, []byte("new"), 0o600)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("WriteFile = %v, want an error matching fs.ErrNotExist", err)
	}

	if want := "state: create temp for " + path + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q does not start with %q", err, want)
	}

	assertEntries(t, dir)
}

func TestSyncDirMissingDirectory(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing")

	err := state.SyncDir(missing)
	if err == nil {
		t.Fatal("SyncDir: want an error for a missing directory, got nil")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SyncDir: want an error matching os.ErrNotExist, got %v", err)
	}
}

func TestSyncDirExistingDirectory(t *testing.T) {
	t.Parallel()

	if err := state.SyncDir(t.TempDir()); err != nil {
		t.Fatalf("SyncDir: %v", err)
	}
}

// TestWriteFileAbortedAfterAnyStepLeavesOldOrNewContent aborts WriteFile at each step of its write
// path in turn. Every step but Create performs its real operation first and then reports an error,
// so the abort lands after the step's effect; Write writes only half the data. Create fails before
// creating anything, because a caller given no file has no name to remove. A kill at the same
// points skips the error path and leaves the temporary file behind; this test covers the error
// path, which must remove it.
func TestWriteFileAbortedAfterAnyStepLeavesOldOrNewContent(t *testing.T) {
	t.Parallel()

	errAbort := errors.New("aborted")

	const (
		oldContent = "old = 1\n"
		newContent = "new = 2\n"
	)

	tests := []struct {
		name  string
		abort func(s *state.Steps)
		want  string
	}{
		{
			name: "create",
			abort: func(s *state.Steps) {
				s.Create = func(string, string) (*os.File, error) { return nil, errAbort }
			},
			want: oldContent,
		},
		{
			name: "write",
			abort: func(s *state.Steps) {
				s.Write = func(f *os.File, data []byte) (int, error) {
					n, err := f.Write(data[:len(data)/2])

					return n, errors.Join(err, errAbort)
				}
			},
			want: oldContent,
		},
		{
			name: "close after write",
			abort: func(s *state.Steps) {
				s.CloseWritten = func(f *os.File) error { return errors.Join(f.Close(), errAbort) }
			},
			want: oldContent,
		},
		{
			name: "open",
			abort: func(s *state.Steps) {
				s.Open = func(name string) (*os.File, error) {
					f, err := os.Open(filepath.Clean(name))
					if err == nil {
						err = f.Close()
					}

					return nil, errors.Join(err, errAbort)
				}
			},
			want: oldContent,
		},
		{
			name: "chmod",
			abort: func(s *state.Steps) {
				s.Chmod = func(f *os.File, mode os.FileMode) error { return errors.Join(f.Chmod(mode), errAbort) }
			},
			want: oldContent,
		},
		{
			name: "sync",
			abort: func(s *state.Steps) {
				s.Sync = func(f *os.File) error { return errors.Join(f.Sync(), errAbort) }
			},
			want: oldContent,
		},
		{
			name: "close after sync",
			abort: func(s *state.Steps) {
				s.CloseSynced = func(f *os.File) error { return errors.Join(f.Close(), errAbort) }
			},
			want: oldContent,
		},
		{
			name: "rename",
			abort: func(s *state.Steps) {
				s.Rename = func(oldpath, newpath string) error {
					return errors.Join(os.Rename(oldpath, newpath), errAbort)
				}
			},
			want: newContent,
		},
		{
			name: "sync directory",
			abort: func(s *state.Steps) {
				s.SyncDir = func(dir string) error { return errors.Join(state.SyncDir(dir), errAbort) }
			},
			want: newContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			writeAt(t, path, oldContent, 0o600)

			s := state.NewSteps()
			tt.abort(&s)

			err := state.WriteFileWith(s, path, []byte(newContent), 0o600)
			if !errors.Is(err, errAbort) {
				t.Fatalf("WriteFile = %v, want an error matching %v", err, errAbort)
			}

			assertFile(t, path, tt.want, 0o600)
			assertEntries(t, dir, "config.toml")
		})
	}
}

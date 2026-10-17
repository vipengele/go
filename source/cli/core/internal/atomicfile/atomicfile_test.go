package atomicfile_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.vipengele.org/cli/core/internal/atomicfile"
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

			if err := atomicfile.Replace(tmp, final, 0o600); err != nil {
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

	if err := atomicfile.Replace(tmp, final, 0o600); err != nil {
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

			if err := atomicfile.Replace(tmp, final, 0o600); err == nil {
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

			if err := atomicfile.WriteFile(path, []byte("key = 1\n"), 0o600); err != nil {
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

	if err := atomicfile.WriteFile(path, []byte("new"), 0o600); err == nil {
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

	if err := atomicfile.WriteFile("config.toml", []byte("key = 1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	assertFile(t, filepath.Join(dir, "config.toml"), "key = 1\n", 0o600)
	assertEntries(t, dir, "config.toml")
}

func TestWriteFileInAMissingDirectoryFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "config.toml")

	err := atomicfile.WriteFile(path, []byte("new"), 0o600)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("WriteFile = %v, want an error matching fs.ErrNotExist", err)
	}

	if want := "atomicfile: create temp for " + path + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q does not start with %q", err, want)
	}

	assertEntries(t, dir)
}

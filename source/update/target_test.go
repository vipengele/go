package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// executableConfig is a valid Config whose Executable reports path.
func executableConfig(t *testing.T, path string) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.Executable = func() (string, error) { return path, nil }

	return c
}

// writeExecutable creates a file named name in dir and returns its path.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("bin"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

// resolvedDir returns dir with every symbolic link resolved, as Target reports paths: a temporary
// directory may itself sit behind a link, as /tmp does on macOS.
func resolvedDir(t *testing.T, dir string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	return resolved
}

// dirEntries returns the names of dir's entries.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func TestTargetRegularFile(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	exe := writeExecutable(t, dir, "atk")

	got, err := executableConfig(t, exe).Target()
	if err != nil {
		t.Fatalf("Target: %v", err)
	}

	if got != exe {
		t.Errorf("Target = %q, want %q", got, exe)
	}
}

func TestTargetSymlink(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	exe := writeExecutable(t, dir, "atk-1.2.3")

	link := filepath.Join(t.TempDir(), "atk")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("creating a symbolic link: %v", err)
	}

	got, err := executableConfig(t, link).Target()
	if err != nil {
		t.Fatalf("Target: %v", err)
	}

	if got != exe {
		t.Errorf("Target = %q, want the link's destination %q", got, exe)
	}
}

func TestTargetRelativeIsAbsolute(t *testing.T) {
	t.Parallel()

	steps := defaultSteps()
	steps.evalSymlinks = func(string) (string, error) { return filepath.Join("bin", "atk"), nil }

	got, err := executableConfig(t, "atk").target(steps)
	if err != nil {
		t.Fatalf("target: %v", err)
	}

	if !filepath.IsAbs(got) {
		t.Errorf("target = %q, want an absolute path", got)
	}

	if want := filepath.Join("bin", "atk"); !strings.HasSuffix(got, want) {
		t.Errorf("target = %q, want a path ending in %q", got, want)
	}
}

func TestTargetDanglingSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	link := filepath.Join(dir, "atk")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Skipf("creating a symbolic link: %v", err)
	}

	got, err := executableConfig(t, link).Target()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Target error = %v, want one matching fs.ErrNotExist", err)
	}

	if got != "" {
		t.Errorf("Target = %q, want the empty path on error", got)
	}

	if !strings.HasPrefix(err.Error(), "update: ") {
		t.Errorf("Target error = %q, want the prefix %q", err, "update: ")
	}
}

func TestTargetMissingPath(t *testing.T) {
	t.Parallel()

	got, err := executableConfig(t, filepath.Join(t.TempDir(), "missing")).Target()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Target error = %v, want one matching fs.ErrNotExist", err)
	}

	if got != "" {
		t.Errorf("Target = %q, want the empty path on error", got)
	}
}

func TestTargetExecutableError(t *testing.T) {
	t.Parallel()

	cause := errors.New("no executable")
	c := validConfig(t.TempDir())
	c.Executable = func() (string, error) { return "", cause }

	got, err := c.Target()
	if !errors.Is(err, cause) {
		t.Errorf("Target error = %v, want one wrapping %v", err, cause)
	}

	if got != "" {
		t.Errorf("Target = %q, want the empty path on error", got)
	}
}

func TestTargetInvalidConfig(t *testing.T) {
	t.Parallel()

	steps := defaultSteps()
	steps.evalSymlinks = func(string) (string, error) {
		t.Error("evalSymlinks called for an invalid Config")
		return "", nil
	}

	c := Config{Executable: func() (string, error) {
		t.Error("Executable called for an invalid Config")
		return "", nil
	}}

	got, err := c.target(steps)
	if err == nil {
		t.Error("target error = nil, want the validation error")
	}

	if got != "" {
		t.Errorf("target = %q, want the empty path on error", got)
	}
}

func TestTargetAbsFailure(t *testing.T) {
	t.Parallel()

	exe := writeExecutable(t, t.TempDir(), "atk")
	cause := errors.New("abs refused")

	steps := defaultSteps()
	steps.abs = func(string) (string, error) { return "", cause }

	got, err := executableConfig(t, exe).target(steps)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "resolving the executable") {
		t.Errorf("target error = %v, want one resolving the executable wrapping %v", err, cause)
	}

	if got != "" {
		t.Errorf("target = %q, want the empty path on error", got)
	}
}

func TestCheckWritable(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	exe := writeExecutable(t, dir, "atk")

	var created string

	steps := defaultSteps()
	steps.createTemp = func(dir, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err == nil {
			created = f.Name()
		}

		return f, err
	}

	if err := executableConfig(t, exe).checkWritable(steps); err != nil {
		t.Fatalf("checkWritable: %v", err)
	}

	if filepath.Dir(created) != dir {
		t.Errorf("probe created in %q, want the executable's directory %q", filepath.Dir(created), dir)
	}

	if base := filepath.Base(created); !strings.HasPrefix(base, "atk-update-") {
		t.Errorf("probe named %q, want the TempPrefix %q", base, "atk-update-")
	}

	if got := dirEntries(t, dir); len(got) != 1 || got[0] != "atk" {
		t.Errorf("directory holds %q after the check, want only the executable", got)
	}
}

func TestCheckWritableTempPrefix(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	c := executableConfig(t, writeExecutable(t, dir, "atk"))
	c.TempPrefix = ".probe-"

	var pattern string

	steps := defaultSteps()
	steps.createTemp = func(dir, p string) (*os.File, error) {
		pattern = p
		return os.CreateTemp(dir, p)
	}

	if err := c.checkWritable(steps); err != nil {
		t.Fatalf("checkWritable: %v", err)
	}

	if pattern != ".probe-*" {
		t.Errorf("pattern = %q, want %q", pattern, ".probe-*")
	}
}

func TestCheckWritableFollowsSymlink(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	exe := writeExecutable(t, dir, "atk")

	link := filepath.Join(t.TempDir(), "atk")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("creating a symbolic link: %v", err)
	}

	var probeDir string

	steps := defaultSteps()
	steps.createTemp = func(d, pattern string) (*os.File, error) {
		probeDir = d
		return os.CreateTemp(d, pattern)
	}

	if err := executableConfig(t, link).checkWritable(steps); err != nil {
		t.Fatalf("checkWritable: %v", err)
	}

	if probeDir != dir {
		t.Errorf("probed %q, want the link destination's directory %q", probeDir, dir)
	}
}

func TestCheckWritablePermission(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	exe := writeExecutable(t, dir, "atk")
	tmp := t.TempDir()

	tests := []struct {
		name   string
		steps  func(fsSteps) fsSteps
		op     string
		inPath string
	}{
		{
			name: "create refused",
			steps: func(s fsSteps) fsSteps {
				s.createTemp = func(dir, _ string) (*os.File, error) {
					return nil, &fs.PathError{Op: "open", Path: dir, Err: fs.ErrPermission}
				}
				return s
			},
			op:     "create",
			inPath: dir,
		},
		{
			name: "remove refused",
			steps: func(s fsSteps) fsSteps {
				s.createTemp = func(_, pattern string) (*os.File, error) {
					return os.CreateTemp(tmp, pattern)
				}
				s.remove = func(name string) error {
					_ = os.Remove(name)
					return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrPermission}
				}
				return s
			},
			op:     "remove",
			inPath: tmp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := executableConfig(t, exe).checkWritable(tt.steps(defaultSteps()))

			var perr *PermissionError
			if !errors.As(err, &perr) {
				t.Fatalf("checkWritable error = %v, want a *PermissionError", err)
			}

			if !errors.Is(err, fs.ErrPermission) {
				t.Errorf("checkWritable error = %v, want one matching fs.ErrPermission", err)
			}

			if perr.Op != tt.op {
				t.Errorf("Op = %q, want %q", perr.Op, tt.op)
			}

			if !strings.HasPrefix(perr.Path, tt.inPath) {
				t.Errorf("Path = %q, want one in %q", perr.Path, tt.inPath)
			}

			if perr.Hint != permissionHint {
				t.Errorf("Hint = %q, want %q", perr.Hint, permissionHint)
			}
		})
	}
}

func TestCheckWritableOtherError(t *testing.T) {
	t.Parallel()

	exe := writeExecutable(t, t.TempDir(), "atk")
	cause := errors.New("disk on fire")

	steps := defaultSteps()
	steps.createTemp = func(string, string) (*os.File, error) { return nil, cause }

	err := executableConfig(t, exe).checkWritable(steps)
	if !errors.Is(err, cause) {
		t.Fatalf("checkWritable error = %v, want one wrapping %v", err, cause)
	}

	var perr *PermissionError
	if errors.As(err, &perr) {
		t.Errorf("checkWritable error = %v, want no *PermissionError", err)
	}
}

func TestCheckWritableInvalidConfig(t *testing.T) {
	t.Parallel()

	steps := defaultSteps()
	steps.evalSymlinks = func(string) (string, error) {
		t.Error("evalSymlinks called for an invalid Config")
		return "", nil
	}
	steps.createTemp = func(string, string) (*os.File, error) {
		t.Error("createTemp called for an invalid Config")
		return nil, errors.New("unreachable")
	}
	steps.remove = func(string) error {
		t.Error("remove called for an invalid Config")
		return nil
	}

	c := Config{Executable: func() (string, error) {
		t.Error("Executable called for an invalid Config")
		return "", nil
	}}

	if err := c.checkWritable(steps); err == nil {
		t.Error("checkWritable error = nil, want the validation error")
	}
}

func TestCheckWritableTargetError(t *testing.T) {
	t.Parallel()

	steps := defaultSteps()
	steps.createTemp = func(string, string) (*os.File, error) {
		t.Error("createTemp called without a target")
		return nil, errors.New("unreachable")
	}

	err := executableConfig(t, filepath.Join(t.TempDir(), "missing")).checkWritable(steps)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("checkWritable error = %v, want one matching fs.ErrNotExist", err)
	}
}

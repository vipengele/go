package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// permissionHint is the [PermissionError.Hint] of a write to the executable's directory that the
// operating system refused.
const permissionHint = "reinstall the tool into a directory you can write to, or rerun the update with elevated privileges"

// fsSteps are the filesystem operations of resolving, probing and replacing the installed
// executable, one field each. [defaultSteps] returns the ones that act on the real filesystem; an
// in-package test substitutes a field to produce a failure the real filesystem cannot, such as a
// permission error while running as root.
type fsSteps struct {
	createTemp   func(dir, pattern string) (*os.File, error)
	remove       func(name string) error
	chmod        func(name string, mode os.FileMode) error
	rename       func(oldpath, newpath string) error
	evalSymlinks func(path string) (string, error)
	stat         func(name string) (os.FileInfo, error)
	abs          func(path string) (string, error)
}

// defaultSteps returns the fsSteps that act on the real filesystem.
func defaultSteps() fsSteps {
	return fsSteps{
		createTemp:   os.CreateTemp,
		remove:       os.Remove,
		chmod:        os.Chmod,
		rename:       os.Rename,
		evalSymlinks: filepath.EvalSymlinks,
		stat:         os.Stat,
		abs:          filepath.Abs,
	}
}

// Target returns the path of the file an update replaces: the running binary, as the Config's
// Executable reports it, with every symbolic link resolved, made absolute. A caller stages the new
// binary in Target's directory, so that the rename into place stays on one filesystem.
//
// The Config is resolved first, so an invalid one fails before anything is read. An error from
// Executable is returned wrapped, and so is the [filepath.EvalSymlinks] error for a path that does
// not exist or a symbolic link that leads nowhere.
func (c Config) Target() (string, error) {
	return c.target(defaultSteps())
}

// target is [Config.Target] through s.
func (c Config) target(s fsSteps) (string, error) {
	r, err := c.resolve()
	if err != nil {
		return "", err
	}

	return r.resolveTarget(s)
}

// resolveTarget is [Config.Target] for a resolved Config, through s.
func (c Config) resolveTarget(s fsSteps) (string, error) {
	exe, err := c.Executable()
	if err != nil {
		return "", fmt.Errorf("update: locating the executable: %w", err)
	}

	resolved, err := s.evalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("update: resolving the executable %q: %w", exe, err)
	}

	abs, err := s.abs(resolved)
	if err != nil {
		return "", fmt.Errorf("update: resolving the executable %q: %w", exe, err)
	}

	return abs, nil
}

// CheckWritable reports whether an update can write to the directory of [Config.Target], by
// creating a file named TempPrefix followed by a random suffix there and removing it again. A
// refusal for lack of permission, creating or removing the file, is a [*PermissionError] whose
// Hint tells the user how to get write access; any other failure is returned wrapped, and so is
// every error of Target.
//
// The check is best-effort, meant to run before a download so that an update which cannot be
// installed is not fetched: the directory may change between it and [Config.Install], and only
// Install's own writes are authoritative.
func (c Config) CheckWritable() error {
	return c.checkWritable(defaultSteps())
}

// checkWritable is [Config.CheckWritable] through s.
func (c Config) checkWritable(s fsSteps) error {
	r, err := c.resolve()
	if err != nil {
		return err
	}

	target, err := r.resolveTarget(s)
	if err != nil {
		return err
	}

	dir := filepath.Dir(target)

	f, err := s.createTemp(dir, r.TempPrefix+"*")
	if err != nil {
		return fsError("create", dir, err)
	}

	name := f.Name()
	_ = f.Close()

	if err := s.remove(name); err != nil {
		return fsError("remove", name, err)
	}

	return nil
}

// fsError is the error a caller sees for op on path failing with err: a [*PermissionError]
// carrying [permissionHint] when err is a permission failure, and err wrapped with op and path
// otherwise.
func fsError(op, path string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return &PermissionError{Path: path, Op: op, Hint: permissionHint, Err: err}
	}

	return fmt.Errorf("update: %s %s: %w", op, path, err)
}

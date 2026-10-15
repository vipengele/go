package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// dirPerm is the mode Ensure gives a directory: readable, writable and searchable by its owner
// alone.
const dirPerm fs.FileMode = 0o700

// Ensure makes sure the directory of each kind exists and is private to its owner, and creates
// every kind's directory when kinds is empty. Repeating a kind is harmless.
//
// A missing directory is created with mode 0700, as are any missing parents; the process umask
// can only narrow that mode. An existing directory whose mode grants anything beyond 0700 has
// those bits cleared, so 0755 becomes 0700 and 0750 becomes 0700, while a narrower mode such as
// 0500 is kept. Parents that already exist are never changed. On the "windows" layout, where
// permission bits do not describe access, an existing directory's mode is left as it is.
//
// Ensure handles every kind before it returns, and returns the failures of all of them joined
// with [errors.Join], each naming its directory: a kind that is not a [Kind], a kind with no
// directory, as in the zero Dirs, a path that exists and is not a directory, and an error from
// creating, inspecting or changing the mode of a directory. A failure can leave a kind's tree
// partly created; calling Ensure again completes it.
func (d Dirs) Ensure(kinds ...Kind) error {
	return d.ensure(os.Chmod, kinds)
}

// ensure is Ensure with the function that changes a directory's mode.
func (d Dirs) ensure(chmod func(string, fs.FileMode) error, kinds []Kind) error {
	if len(kinds) == 0 {
		kinds = []Kind{Config, State, Cache, Data, Log}
	}
	var errs []error
	seen := make(map[Kind]bool, len(kinds))
	for _, k := range kinds {
		if seen[k] {
			continue
		}
		seen[k] = true
		if err := d.ensureKind(chmod, k); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ensureKind creates the directory of k, or narrows the mode of the existing one.
func (d Dirs) ensureKind(chmod func(string, fs.FileMode) error, k Kind) error {
	if !k.valid() {
		return fmt.Errorf("paths: ensure: invalid kind %v", k)
	}
	dir := d.dir(k)
	if dir == "" {
		return fmt.Errorf("paths: ensure %v: no directory resolved", k)
	}

	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("paths: ensure %v directory %s: %w", k, dir, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("paths: ensure %v directory %s: %w", k, dir, err)
	case !info.IsDir():
		return fmt.Errorf("paths: ensure %v directory %s: exists and is not a directory", k, dir)
	}

	perm := info.Mode().Perm()
	if d.goos == "windows" || perm&^dirPerm == 0 {
		return nil
	}
	if err := chmod(dir, perm&dirPerm); err != nil {
		return fmt.Errorf("paths: ensure %v directory %s: %w", k, dir, err)
	}
	return nil
}

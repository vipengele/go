package update

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"syscall"
)

// backupSuffix follows the executable's path to name the backup of the previous binary.
const backupSuffix = ".old"

// Windows error codes a remove of a running executable's file fails with. The values are those of
// the Windows API, written out because package syscall defines the names on Windows alone.
const (
	windowsAccessDenied     syscall.Errno = 5
	windowsSharingViolation syscall.Errno = 32
)

// errNoStaged reports a nil [*Staged] passed to [Config.Install].
var errNoStaged = errors.New("update: no staged binary to install")

// Install replaces the executable [Config.Target] names with the staged binary, keeping the
// previous binary as a backup until the returned [*Installed] settles it. Install owns staged: it
// settles it whether it succeeds or fails, and on every failure the staged file is removed, with an
// error removing it joined onto the error returned.
//
// The steps run in this order, and each failure stops the ones after it:
//
//  1. The Config is resolved and the executable located as Target does. A symbolic link leading
//     nowhere, or a path that does not exist, returns the wrapped [filepath.EvalSymlinks] error.
//  2. The executable's mode is read.
//  3. The staged file's directory, its symbolic links resolved, must be the executable's
//     directory, so that both renames stay in one directory on one filesystem. Extract it into the
//     directory of Target.
//  4. staged is kept: closed and left on disk.
//  5. The staged file takes the executable's permission bits; setuid, setgid and sticky are not
//     copied. This happens before any rename, so the binary never sits at the executable's path
//     with the staged mode.
//  6. The executable is renamed to its backup, the executable's path followed by ".old". A backup
//     already there is replaced.
//  7. The staged file is renamed to the executable's path.
//
// Nothing at the executable's path changes before step 6. When step 7 fails, the backup is renamed
// back first; when that fails too, the error is a [*RestoreError] whose Backup names the file
// holding the previous binary. A failure the operating system reports as a lack of permission, at
// any step that writes, is a [*PermissionError] carrying a hint for the user.
//
// The two renames are load-bearing: Windows lets a running executable be renamed but not
// overwritten or removed, so the old binary moves aside before the new one moves in, and a single
// rename over the executable, or a copy onto it, fails there. Install has one code path for every
// operating system, and its behaviour on Windows is unverified.
//
// Only the path the executable resolves to is replaced: a symbolic link to it keeps pointing at
// the new binary, but another hard link to the same file keeps the previous binary.
//
// The [*Installed] lives in memory alone. A backup left behind by a process that ended before
// settling it is invisible to this package, and the next Install replaces it.
func (c Config) Install(staged *Staged) (*Installed, error) {
	return c.install(staged, defaultSteps())
}

// install is [Config.Install] through s.
func (c Config) install(staged *Staged, s fsSteps) (*Installed, error) {
	if staged == nil {
		return nil, errNoStaged
	}

	name := staged.Name()

	// abandon settles staged and removes its file by path, joining both errors onto err. Keep
	// does nothing once staged is already kept.
	abandon := func(err error) error {
		closeErr := staged.Keep()

		var removeErr error
		if rmErr := s.remove(name); rmErr != nil {
			removeErr = fmt.Errorf("update: removing staged binary: %w", rmErr)
		}

		return errors.Join(err, closeErr, removeErr)
	}

	r, err := c.resolve()
	if err != nil {
		return nil, abandon(err)
	}

	target, err := r.resolveTarget(s)
	if err != nil {
		return nil, abandon(err)
	}

	info, err := s.stat(target)
	if err != nil {
		return nil, abandon(fsError("stat", target, err))
	}

	if err := checkStagedDir(name, target, s); err != nil {
		return nil, abandon(err)
	}

	if err := staged.Keep(); err != nil {
		return nil, abandon(err)
	}

	if err := s.chmod(name, info.Mode().Perm()); err != nil {
		return nil, abandon(fsError("chmod", name, err))
	}

	backup := target + backupSuffix

	if err := s.rename(target, backup); err != nil {
		return nil, abandon(fsError("rename", target, err))
	}

	if err := s.rename(name, target); err != nil {
		installErr := fsError("rename", name, err)

		if restoreErr := s.rename(backup, target); restoreErr != nil {
			return nil, abandon(&RestoreError{
				Path:       target,
				Backup:     backup,
				Err:        installErr,
				RestoreErr: fsError("rename", backup, restoreErr),
			})
		}

		return nil, abandon(installErr)
	}

	return &Installed{path: target, backup: backup, goos: r.GOOS, steps: s}, nil
}

// checkStagedDir returns nil when the directory of the staged file name, its symbolic links
// resolved and made absolute, is the directory of target, which is already resolved.
func checkStagedDir(name, target string, s fsSteps) error {
	dir := filepath.Dir(name)

	resolved, err := s.evalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("update: resolving the staged binary's directory %q: %w", dir, err)
	}

	abs, err := filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("update: resolving the staged binary's directory %q: %w", dir, err)
	}

	if want := filepath.Dir(target); abs != want {
		return fmt.Errorf("update: staged binary %q is not in the executable's directory %q", name, want)
	}

	return nil
}

// Installed is a binary [Config.Install] has moved into place, with the previous binary kept as a
// backup beside it. Exactly one of [Installed.Discard] and [Installed.Rollback] settles it;
// whichever is called first decides, and every later call of either does nothing and returns nil.
// Leaving it unsettled leaves the backup on disk.
//
// Installed is held in memory alone: a backup orphaned by a process that ended before settling it
// is invisible to this package, and the next Install replaces it.
//
// An Installed is safe for concurrent use.
type Installed struct {
	path   string
	backup string
	// goos is the resolved GOOS of the Config, which decides how a failed removal of the backup
	// is read.
	goos  string
	steps fsSteps

	mu      sync.Mutex
	settled bool
}

// Path is the path the new binary was installed at: the executable with its symbolic links
// resolved. A [PostUpdate] hook receives it.
func (i *Installed) Path() string {
	return i.path
}

// Backup is the path of the file holding the previous binary, Path followed by ".old".
func (i *Installed) Backup() string {
	return i.backup
}

// Discard removes the backup, keeping the new binary. When the backup cannot be removed because a
// process holds it open, as Windows does for the file of a running executable such as the process
// that ran the update, the error is a [*BackupLockedError]: the update has succeeded, the caller
// may ignore it, and the next Install replaces the backup. Any other failure is a
// [*PermissionError] for a lack of permission and a wrapped error otherwise. Once Discard or
// Rollback has been called, Discard does nothing and returns nil.
func (i *Installed) Discard() error {
	if !i.settle() {
		return nil
	}

	err := i.steps.remove(i.backup)
	if err == nil {
		return nil
	}

	if backupLocked(i.goos, err) {
		return &BackupLockedError{Path: i.backup, Err: err}
	}

	return fsError("remove", i.backup, err)
}

// Rollback renames the backup back over Path, putting the previous binary in place and removing
// the new one. A failure is a [*PermissionError] for a lack of permission and a wrapped error
// otherwise; the backup then stays where Backup names it. Once Discard or Rollback has been
// called, Rollback does nothing and returns nil.
func (i *Installed) Rollback() error {
	if !i.settle() {
		return nil
	}

	if err := i.steps.rename(i.backup, i.path); err != nil {
		return fsError("rename", i.backup, err)
	}

	return nil
}

// settle marks i settled and reports whether this call is the one that did.
func (i *Installed) settle() bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.settled {
		return false
	}

	i.settled = true

	return true
}

// backupLocked reports whether err, from removing the backup on goos, is the failure of a file
// another process holds open: on Windows a sharing violation, or the access denial a removal of a
// running executable's file returns. No other operating system refuses to remove an open file.
func backupLocked(goos string, err error) bool {
	if goos != "windows" {
		return false
	}

	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}

	return errno == windowsSharingViolation || errno == windowsAccessDenied
}

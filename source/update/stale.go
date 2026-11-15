package update

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

// RemoveStaleBackup removes the backup [Config.Install] keeps beside the executable, the path of
// [Config.Target] followed by ".old", and returns nil when there is none. On Windows a backup holds
// the file of the process that ran the update, which cannot be removed while that process runs, so
// [Installed.Discard] leaves it behind; a later process removes it here.
//
// A backup that cannot be removed because a process holds it open is a [*BackupLockedError], and
// the backup stays on disk. Any other failure to remove it is a [*PermissionError] for a lack of
// permission and a wrapped error otherwise, and so is every error of Target.
//
// RemoveStaleBackup removes whatever backup is there, on every operating system, unless
// [Config.BackupGuard] says it is in use: then the backup stays and the result is nil. A guard
// that fails leaves the backup in place too, and its error, wrapped, is the result. Without a
// guard it must not be called while an update is unsettled, between Install and
// [Installed.Discard] or [Installed.Rollback], on a host that can remove an open file: it would
// remove the backup Rollback needs. [Config.UpdateCheckHooks] calls it only on Windows, where the
// backup of an unsettled update in the running process is its own file and cannot be removed, and
// a backup that outlives the process, as an update awaiting confirmation keeps one, is the
// guard's to protect. ctx reaches the guard.
func (c Config) RemoveStaleBackup(ctx context.Context) error {
	return c.removeStaleBackup(ctx, defaultSteps())
}

// removeStaleBackupOnHost is [Config.RemoveStaleBackup] on a host running goos, and does nothing
// unless goos is "windows". Off Windows Discard always removes the backup and a stale one is left
// only by a crash, which the next Install replaces, while a removal could take the backup of an
// unsettled update from Rollback.
func (c Config) removeStaleBackupOnHost(ctx context.Context, goos string) error {
	if goos != "windows" {
		return nil
	}

	return c.RemoveStaleBackup(ctx)
}

// removeStaleBackup is [Config.RemoveStaleBackup] through s.
func (c Config) removeStaleBackup(ctx context.Context, s fsSteps) error {
	r, err := c.resolve()
	if err != nil {
		return err
	}

	if c.BackupGuard != nil {
		inUse, err := c.BackupGuard.BackupInUse(ctx)
		if err != nil {
			return fmt.Errorf("update: check whether the backup is in use: %w", err)
		}

		if inUse {
			return nil
		}
	}

	target, err := r.resolveTarget(s)
	if err != nil {
		return err
	}

	backup := target + backupSuffix

	err = s.remove(backup)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if backupLocked(r.GOOS, err) {
		return &BackupLockedError{Path: backup, Err: err}
	}

	return fsError("remove", backup, err)
}

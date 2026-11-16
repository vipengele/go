package selfupdate

import (
	"errors"
	"fmt"
	"io/fs"
)

// failedSuffix names the file a rollback leaves the replaced binary at, <target>.failed, where the
// running image cannot be replaced in place. Boot removes it once no update is pending.
const failedSuffix = ".failed"

// restoreBackup puts the backup the record names back over its target, by the step of the
// platform. A backup that is absent is a *NoBackupError, with nothing touched; a failed check or
// step is a *RestoreError.
func restoreBackup(ops fsOps, rec Record) error {
	if _, err := ops.lstat(rec.Backup); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &NoBackupError{Path: rec.Backup}
		}

		return &RestoreError{Backup: rec.Backup, Target: rec.Target, Err: err}
	}

	return platformRestore(ops, rec)
}

// restoreInPlace renames the backup over the target in one atomic step, which needs a file system
// that lets the image of a running process be replaced.
func restoreInPlace(ops fsOps, rec Record) error {
	if err := ops.rename(rec.Backup, rec.Target); err != nil {
		return &RestoreError{Backup: rec.Backup, Target: rec.Target, Err: err}
	}

	return nil
}

// restoreAside moves the target aside to <target>.failed and then renames the backup to the
// target, for a file system that lets the image of a running process be renamed but not replaced.
// A stale <target>.failed is removed first. An absent target skips the move aside. When the backup
// cannot be renamed, the target is moved back from <target>.failed, and the error carries the
// failure of that undo too, so a target left at <target>.failed is visible.
func restoreAside(ops fsOps, rec Record) error {
	failed := rec.Target + failedSuffix

	if err := ops.remove(failed); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &RestoreError{Backup: rec.Backup, Target: rec.Target,
			Err: fmt.Errorf("remove stale %s: %w", failed, err)}
	}

	_, err := ops.lstat(rec.Target)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return restoreInPlace(ops, rec)
	case err != nil:
		return &RestoreError{Backup: rec.Backup, Target: rec.Target, Err: err}
	}

	if err := ops.rename(rec.Target, failed); err != nil {
		return &RestoreError{Backup: rec.Backup, Target: rec.Target,
			Err: fmt.Errorf("move %s aside to %s: %w", rec.Target, failed, err)}
	}

	if err := ops.rename(rec.Backup, rec.Target); err != nil {
		if undoErr := ops.rename(failed, rec.Target); undoErr != nil {
			err = errors.Join(err, fmt.Errorf("move %s back to %s: %w", failed, rec.Target, undoErr))
		}

		return &RestoreError{Backup: rec.Backup, Target: rec.Target, Err: err}
	}

	return nil
}

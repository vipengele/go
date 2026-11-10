package update

import (
	"errors"
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
// RemoveStaleBackup removes whatever backup is there, on every operating system. It must not be
// called while an update is unsettled, between Install and [Installed.Discard] or
// [Installed.Rollback], on a host that can remove an open file: it would remove the backup
// Rollback needs. [Config.UpdateCheckHooks] calls it only on Windows, where the backup of an
// unsettled update is the running process's own file and cannot be removed.
func (c Config) RemoveStaleBackup() error {
	return c.removeStaleBackup(defaultSteps())
}

// removeStaleBackupOnHost is [Config.RemoveStaleBackup] on a host running goos, and does nothing
// unless goos is "windows". Off Windows Discard always removes the backup and a stale one is left
// only by a crash, which the next Install replaces, while a removal could take the backup of an
// unsettled update from Rollback.
func (c Config) removeStaleBackupOnHost(goos string) error {
	if goos != "windows" {
		return nil
	}

	return c.RemoveStaleBackup()
}

// removeStaleBackup is [Config.RemoveStaleBackup] through s.
func (c Config) removeStaleBackup(s fsSteps) error {
	r, err := c.resolve()
	if err != nil {
		return err
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

package selfupdate

import (
	"errors"
	"fmt"
)

// ErrUpdatePending reports a new update begun while the stored record is still pending: the
// previous update has been neither confirmed nor rolled back.
var ErrUpdatePending = errors.New("selfupdate: an update is already pending")

// ErrNotUpdatedBuild reports that the running version is not the ToVersion of the pending record,
// so this process is not the build the update installed.
var ErrNotUpdatedBuild = errors.New("selfupdate: running version is not the pending update's")

// ErrNoPendingUpdate reports an update abandoned while the stored record is not pending: there is
// no update to abandon.
var ErrNoPendingUpdate = errors.New("selfupdate: no update is pending")

// ErrUpdateRunning reports an update abandoned while the running version is the ToVersion of the
// pending record: the binary was swapped and this process is the build the update installed, so
// only a boot judges it.
var ErrUpdateRunning = errors.New("selfupdate: running version is the pending update's")

// NoBackupError reports a rollback that found no backup binary at Path, so nothing could be
// restored and the target was left as it is.
type NoBackupError struct {
	// Path is the backup the record names.
	Path string
}

// Error names the missing backup.
func (e *NoBackupError) Error() string {
	return fmt.Sprintf("selfupdate: no backup at %s", e.Path)
}

// BackupRemovalError reports that an update was recorded as confirmed but its backup at Path could
// not be removed. The confirmation stands; the backup stays on disk.
type BackupRemovalError struct {
	// Path is the backup that was not removed.
	Path string
	// Err is the removal's error.
	Err error
}

// Error names the backup and the removal's error.
func (e *BackupRemovalError) Error() string {
	return fmt.Sprintf("selfupdate: update confirmed, but removing backup %s failed: %v", e.Path, e.Err)
}

// Unwrap returns Err.
func (e *BackupRemovalError) Unwrap() error {
	return e.Err
}

// RestoreError reports that a rollback could not rename the backup at Backup over the target at
// Target. The record is not marked rolled back.
type RestoreError struct {
	// Backup is the binary that was to be restored.
	Backup string
	// Target is the path it was to be restored to.
	Target string
	// Err is the rename's error.
	Err error
}

// Error names both paths and the rename's error.
func (e *RestoreError) Error() string {
	return fmt.Sprintf("selfupdate: restore %s to %s: %v", e.Backup, e.Target, e.Err)
}

// Unwrap returns Err.
func (e *RestoreError) Unwrap() error {
	return e.Err
}

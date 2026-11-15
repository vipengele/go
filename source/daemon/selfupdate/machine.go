package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"go.vipengele.org/daemon"
)

// failedSuffix names the file a rollback leaves the replaced binary at, <target>.failed, where the
// running image cannot be replaced in place. Boot removes it once no update is pending.
const failedSuffix = ".failed"

// Pending describes an update Begin records: the binary at Target is about to be replaced by
// ToVersion, and the previous binary is kept at Backup.
type Pending struct {
	// FromVersion is the version running when the update begins.
	FromVersion string
	// ToVersion is the version being installed. It is required.
	ToVersion string
	// Target is the path of the binary being replaced. It is required.
	Target string
	// Backup is the path the previous binary is kept at. It is required.
	Backup string
}

// Machine moves a Record in a Store through the statuses of a self-update, judged against the
// version of the running build. Each method reads and writes the record within one Store.Update,
// so steps run by different processes never interleave.
type Machine struct {
	store           Store
	running         string
	clock           daemon.Clock
	maxBootAttempts int

	// restore puts the backup the record names back over its target. It returns *NoBackupError
	// when the backup is absent, having touched nothing.
	restore func(Record) error
	// removeFile removes the file at a path, as os.Remove does.
	removeFile func(string) error
}

// New returns a Machine over store for a build running runningVersion. It refuses a nil store and
// reports the error of any Option that rejects its argument.
func New(store Store, runningVersion string, opts ...Option) (*Machine, error) {
	if store == nil {
		return nil, errors.New("selfupdate: nil store")
	}

	m := &Machine{
		store:           store,
		running:         runningVersion,
		clock:           systemClock{},
		maxBootAttempts: defaultMaxBootAttempts,
		restore:         restoreBackup,
		removeFile:      os.Remove,
	}

	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// State returns the stored Record.
func (m *Machine) State(ctx context.Context) (Record, error) {
	rec, err := m.store.Load(ctx)
	if err != nil {
		return Record{}, fmt.Errorf("selfupdate: state: %w", err)
	}

	return rec, nil
}

// BackupInUse reports whether an update is pending, so that its backup must be kept. A record
// that cannot be read is an error, on which a caller keeps the backup.
func (m *Machine) BackupInUse(ctx context.Context) (bool, error) {
	rec, err := m.store.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("selfupdate: backup in use: %w", err)
	}

	return rec.Status == StatusPending, nil
}

// Begin records the update p as pending, with no boot attempts, started now. It returns
// ErrUpdatePending while another update is pending, and an error when p lacks ToVersion, Target
// or Backup.
func (m *Machine) Begin(ctx context.Context, p Pending) error {
	switch {
	case p.ToVersion == "":
		return errors.New("selfupdate: begin: empty ToVersion")
	case p.Target == "":
		return errors.New("selfupdate: begin: empty Target")
	case p.Backup == "":
		return errors.New("selfupdate: begin: empty Backup")
	}

	return m.change(ctx, "begin", func(rec Record) (Record, bool, error) {
		if rec.Status == StatusPending {
			return rec, false, ErrUpdatePending
		}

		now := m.clock.Now()

		return Record{
			Status:      StatusPending,
			FromVersion: p.FromVersion,
			ToVersion:   p.ToVersion,
			Target:      p.Target,
			Backup:      p.Backup,
			StartedAt:   now,
			UpdatedAt:   now,
		}, true, nil
	})
}

// Boot judges a boot of the running build and reports whether it rolled the update back, in which
// case the caller exits so the restored binary is started.
//
// With no update pending, it removes a leftover <target>.failed, ignoring any error, and changes
// nothing. With an update pending for another version than the running one, the update is not
// what runs (it never installed, its rollback was not recorded, or the binary was replaced), so
// Boot records rolled_back without touching a file or counting an attempt. With the running
// version pending, Boot counts and stores the attempt first, so that a build that crashes before
// confirming still uses it up, and rolls back once the count passes the limit of
// WithMaxBootAttempts. A failed store of the attempt is an error and never rolls back.
//
// A rollback restores the backup before it records rolled_back. A restore that fails, a missing
// backup included, is returned and leaves the record pending. When the restore succeeds and the
// record cannot be stored, Boot returns true with the error: the backup is in place, and the next
// boot, running the restored version, records rolled_back.
func (m *Machine) Boot(ctx context.Context) (bool, error) {
	var (
		pending bool
		target  string
		counted Record
	)

	err := m.change(ctx, "boot", func(rec Record) (Record, bool, error) {
		switch {
		case rec.Status != StatusPending:
			target = rec.Target
			return rec, false, nil
		case rec.ToVersion != m.running:
			rec.Status = StatusRolledBack
			rec.Reason = m.notRunningReason(rec)
			rec.UpdatedAt = m.clock.Now()

			return rec, true, nil
		}

		pending = true
		rec.BootAttempts++
		rec.UpdatedAt = m.clock.Now()
		counted = rec

		return rec, true, nil
	})
	if err != nil {
		return false, err
	}

	if !pending {
		if target != "" {
			_ = m.removeFile(target + failedSuffix)
		}

		return false, nil
	}

	if counted.BootAttempts <= m.maxBootAttempts {
		return false, nil
	}

	reason := fmt.Sprintf("boot attempt %d of %s exceeds the limit of %d", counted.BootAttempts,
		counted.ToVersion, m.maxBootAttempts)

	return m.rollBack(ctx, "boot", reason)
}

// MarkFailed rolls the pending update back at once, whatever its boot attempts, recording reason,
// and reports whether it did. It returns ErrNotUpdatedBuild when the running version is not the
// pending one, and false with a nil error when no update is pending. A failed restore or store is
// reported as Boot reports it.
func (m *Machine) MarkFailed(ctx context.Context, reason string) (bool, error) {
	return m.rollBack(ctx, "mark failed", reason)
}

// Confirm records the pending update as confirmed and then removes its backup. It does nothing
// when no update is pending and returns ErrNotUpdatedBuild when the running version is not the
// pending one. A backup already gone counts as removed; any other removal failure is a
// *BackupRemovalError, and the record stays confirmed.
func (m *Machine) Confirm(ctx context.Context) error {
	var backup string

	confirmed := false

	err := m.change(ctx, "confirm", func(rec Record) (Record, bool, error) {
		switch {
		case rec.Status != StatusPending:
			return rec, false, nil
		case rec.ToVersion != m.running:
			return rec, false, ErrNotUpdatedBuild
		}

		confirmed = true
		backup = rec.Backup
		rec.Status = StatusConfirmed
		rec.UpdatedAt = m.clock.Now()

		return rec, true, nil
	})
	if err != nil || !confirmed || backup == "" {
		return err
	}

	if err := m.removeFile(backup); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &BackupRemovalError{Path: backup, Err: err}
	}

	return nil
}

// rollBack restores the backup of the update pending for the running version and records it
// rolled_back with reason, both under one Store.Update, and reports whether the backup was
// restored. With no update pending it returns false and a nil error; with an update pending for
// another version, ErrNotUpdatedBuild.
func (m *Machine) rollBack(ctx context.Context, op, reason string) (bool, error) {
	restored := false

	err := m.change(ctx, op, func(rec Record) (Record, bool, error) {
		switch {
		case rec.Status != StatusPending:
			return rec, false, nil
		case rec.ToVersion != m.running:
			return rec, false, ErrNotUpdatedBuild
		}

		if err := m.restore(rec); err != nil {
			return rec, false, err
		}

		restored = true
		rec.Status = StatusRolledBack
		rec.Reason = reason
		rec.UpdatedAt = m.clock.Now()

		return rec, true, nil
	})

	return restored, err
}

// notRunningReason says why a record pending for another version than the running one is rolled
// back without a restore.
func (m *Machine) notRunningReason(rec Record) string {
	if rec.FromVersion != "" && m.running == rec.FromVersion {
		return fmt.Sprintf("%s is running instead of %s: the update did not install, or its rollback was not recorded",
			m.running, rec.ToVersion)
	}

	return fmt.Sprintf("%s is running instead of %s: the binary was replaced outside the update",
		m.running, rec.ToVersion)
}

// change applies fn to the stored record under one Store.Update. fn returns the next record,
// whether to store it, and an error that aborts the update. change returns fn's error as it is,
// stores nothing when fn asks for no write, and wraps any error of the store with op.
func (m *Machine) change(ctx context.Context, op string, fn func(Record) (Record, bool, error)) error {
	// unchanged aborts the Update without a write: an fn error is the only way to make a Store
	// write nothing.
	unchanged := errors.New("selfupdate: record unchanged")

	var fnErr error

	err := m.store.Update(ctx, func(rec Record) (Record, error) {
		next, write, err := fn(rec)

		switch {
		case err != nil:
			fnErr = err
			return rec, err
		case !write:
			return rec, unchanged
		default:
			return next, nil
		}
	})

	switch {
	case err == nil, errors.Is(err, unchanged):
		return nil
	case fnErr != nil && errors.Is(err, fnErr):
		return fnErr
	default:
		return fmt.Errorf("selfupdate: %s: %w", op, err)
	}
}

// restoreBackup renames the backup the record names over its target. A backup that is absent is a
// *NoBackupError, with nothing touched; a failed check or rename is a *RestoreError.
func restoreBackup(rec Record) error {
	if _, err := os.Lstat(rec.Backup); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &NoBackupError{Path: rec.Backup}
		}

		return &RestoreError{Backup: rec.Backup, Target: rec.Target, Err: err}
	}

	if err := os.Rename(rec.Backup, rec.Target); err != nil {
		return &RestoreError{Backup: rec.Backup, Target: rec.Target, Err: err}
	}

	return nil
}

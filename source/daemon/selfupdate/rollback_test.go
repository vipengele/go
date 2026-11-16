package selfupdate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// binaryRecord returns a record pending in dir, with target holding "new" and, when backup is
// set, the backup holding "old".
func binaryRecord(t *testing.T, dir string, backup bool) Record {
	t.Helper()

	rec := pendingRecord()
	rec.Target = filepath.Join(dir, "tool")
	rec.Backup = filepath.Join(dir, "tool.old")
	writeRaw(t, rec.Target, "new")

	if backup {
		writeRaw(t, rec.Backup, "old")
	}

	return rec
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Lstat(%s) = %v, want it absent", path, err)
	}
}

func TestRestorePutsTheBackupOverTheTarget(t *testing.T) {
	t.Parallel()

	for name, restore := range map[string]func(fsOps, Record) error{
		"platform": restoreBackup,
		"in place": restoreInPlace,
		"aside":    restoreAside,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := binaryRecord(t, t.TempDir(), true)

			if err := restore(osFSOps(), rec); err != nil {
				t.Fatalf("restore: %v", err)
			}

			if got := readRaw(t, rec.Target); got != "old" {
				t.Errorf("target = %q, want the backup's %q", got, "old")
			}

			assertNotExist(t, rec.Backup)
		})
	}
}

func TestRestoreAsideKeepsTheReplacedTargetAtFailed(t *testing.T) {
	t.Parallel()

	rec := binaryRecord(t, t.TempDir(), true)
	failed := rec.Target + failedSuffix
	writeRaw(t, failed, "stale")

	if err := restoreAside(osFSOps(), rec); err != nil {
		t.Fatalf("restoreAside: %v", err)
	}

	if got := readRaw(t, rec.Target); got != "old" {
		t.Errorf("target = %q, want the backup's %q", got, "old")
	}

	if got := readRaw(t, failed); got != "new" {
		t.Errorf("%s = %q, want the replaced target's %q", failed, got, "new")
	}

	assertNotExist(t, rec.Backup)
}

func TestRestoreAsideWithNoTargetRenamesTheBackup(t *testing.T) {
	t.Parallel()

	rec := binaryRecord(t, t.TempDir(), true)
	if err := os.Remove(rec.Target); err != nil {
		t.Fatal(err)
	}

	if err := restoreAside(osFSOps(), rec); err != nil {
		t.Fatalf("restoreAside: %v", err)
	}

	if got := readRaw(t, rec.Target); got != "old" {
		t.Errorf("target = %q, want the backup's %q", got, "old")
	}

	assertNotExist(t, rec.Backup)
	assertNotExist(t, rec.Target+failedSuffix)
}

func TestRestoreWithNoBackupTouchesNothing(t *testing.T) {
	t.Parallel()

	rec := binaryRecord(t, t.TempDir(), false)

	err := restoreBackup(osFSOps(), rec)

	var noBackup *NoBackupError
	if !errors.As(err, &noBackup) || noBackup.Path != rec.Backup {
		t.Fatalf("restoreBackup = %v, want a *NoBackupError for %s", err, rec.Backup)
	}

	if got := readRaw(t, rec.Target); got != "new" {
		t.Errorf("target = %q, want it untouched", got)
	}

	assertNotExist(t, rec.Target+failedSuffix)
}

func TestMarkFailedWithTheDefaultRestore(t *testing.T) {
	t.Parallel()

	rec := binaryRecord(t, t.TempDir(), true)
	store := storeWith(t, rec)

	m, err := New(store, newVersion, WithClock(fakeClock{now: testTime()}))
	if err != nil {
		t.Fatal(err)
	}

	rolledBack, err := m.MarkFailed(t.Context(), "broken")
	if err != nil || !rolledBack {
		t.Fatalf("MarkFailed = %v, %v, want true and no error", rolledBack, err)
	}

	if got := readRaw(t, rec.Target); got != "old" {
		t.Errorf("target = %q, want the backup's %q", got, "old")
	}

	assertNotExist(t, rec.Backup)

	if got := load(t, store); got.Status != StatusRolledBack {
		t.Errorf("status = %v, want rolled_back", got.Status)
	}
}

func TestBootWithNoUpdatePendingRemovesTheFailedTarget(t *testing.T) {
	t.Parallel()

	rec := binaryRecord(t, t.TempDir(), false)
	rec.Status = StatusRolledBack
	failed := rec.Target + failedSuffix
	writeRaw(t, failed, "new")

	m, err := New(storeWith(t, rec), oldVersion, WithClock(fakeClock{now: testTime()}))
	if err != nil {
		t.Fatal(err)
	}

	if rolledBack, err := m.Boot(t.Context()); err != nil || rolledBack {
		t.Fatalf("Boot = %v, %v, want false and no error", rolledBack, err)
	}

	assertNotExist(t, failed)

	if got := readRaw(t, rec.Target); got != "new" {
		t.Errorf("target = %q, want it untouched", got)
	}
}

// scriptedOps records the calls made through an fsOps and fails the renames and removes it is
// told to. Every path exists to lstat unless it is in absent.
type scriptedOps struct {
	calls      []string
	renameErrs map[string]error
	removeErr  error
	absent     map[string]bool
}

func (s *scriptedOps) ops() fsOps {
	return fsOps{
		rename: func(oldpath, newpath string) error {
			call := fmt.Sprintf("rename %s %s", oldpath, newpath)
			s.calls = append(s.calls, call)

			return s.renameErrs[call]
		},
		remove: func(name string) error {
			s.calls = append(s.calls, "remove "+name)

			return s.removeErr
		},
		lstat: func(name string) (fs.FileInfo, error) {
			if s.absent[name] {
				return nil, fs.ErrNotExist
			}

			return nil, nil
		},
	}
}

func TestRestoreAsideUndoesTheMoveWhenTheBackupCannotBeRenamed(t *testing.T) {
	t.Parallel()

	rec := pendingRecord()
	failed := rec.Target + failedSuffix
	renameErr := errors.New("backup locked")
	undoErr := errors.New("target locked")

	for name, tc := range map[string]struct {
		undoErr error
		want    []error
	}{
		"undone":         {want: []error{renameErr}},
		"undo fails too": {undoErr: undoErr, want: []error{renameErr, undoErr}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := &scriptedOps{renameErrs: map[string]error{
				"rename " + rec.Backup + " " + rec.Target: renameErr,
				"rename " + failed + " " + rec.Target:     tc.undoErr,
			}}

			err := restoreAside(s.ops(), rec)

			var restoreErr *RestoreError
			if !errors.As(err, &restoreErr) || restoreErr.Backup != rec.Backup || restoreErr.Target != rec.Target {
				t.Fatalf("restoreAside = %v, want a *RestoreError for %s and %s", err, rec.Backup, rec.Target)
			}

			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Errorf("restoreAside = %v, want it to carry %v", err, want)
				}
			}

			wantCalls := []string{
				"remove " + failed,
				"rename " + rec.Target + " " + failed,
				"rename " + rec.Backup + " " + rec.Target,
				"rename " + failed + " " + rec.Target,
			}
			if !slices.Equal(s.calls, wantCalls) {
				t.Errorf("calls = %q, want %q", s.calls, wantCalls)
			}
		})
	}
}

func TestRestoreAsideStopsWhenTheTargetCannotBeMoved(t *testing.T) {
	t.Parallel()

	rec := pendingRecord()
	failed := rec.Target + failedSuffix
	moveErr := errors.New("target locked")
	s := &scriptedOps{renameErrs: map[string]error{"rename " + rec.Target + " " + failed: moveErr}}

	var restoreErr *RestoreError
	if err := restoreAside(s.ops(), rec); !errors.As(err, &restoreErr) || !errors.Is(err, moveErr) {
		t.Fatalf("restoreAside = %v, want a *RestoreError carrying %v", err, moveErr)
	}

	wantCalls := []string{"remove " + failed, "rename " + rec.Target + " " + failed}
	if !slices.Equal(s.calls, wantCalls) {
		t.Errorf("calls = %q, want %q", s.calls, wantCalls)
	}
}

func TestRestoreAsideStopsWhenTheStaleFailedCannotBeRemoved(t *testing.T) {
	t.Parallel()

	rec := pendingRecord()
	removeErr := errors.New("access denied")
	s := &scriptedOps{removeErr: removeErr}

	var restoreErr *RestoreError
	if err := restoreAside(s.ops(), rec); !errors.As(err, &restoreErr) || !errors.Is(err, removeErr) {
		t.Fatalf("restoreAside = %v, want a *RestoreError carrying %v", err, removeErr)
	}

	if wantCalls := []string{"remove " + rec.Target + failedSuffix}; !slices.Equal(s.calls, wantCalls) {
		t.Errorf("calls = %q, want %q", s.calls, wantCalls)
	}
}

func TestRestoreAsideIgnoresAnAbsentStaleFailed(t *testing.T) {
	t.Parallel()

	rec := pendingRecord()
	s := &scriptedOps{removeErr: fs.ErrNotExist}

	if err := restoreAside(s.ops(), rec); err != nil {
		t.Fatalf("restoreAside: %v", err)
	}

	if n := len(s.calls); n != 3 {
		t.Errorf("calls = %q, want the remove and both renames", s.calls)
	}
}

func TestRestoreReportsAFailedBackupCheck(t *testing.T) {
	t.Parallel()

	rec := pendingRecord()
	statErr := errors.New("permission denied")
	ops := fsOps{
		lstat: func(string) (fs.FileInfo, error) { return nil, statErr },
		rename: func(string, string) error {
			t.Error("rename called after a failed backup check")
			return nil
		},
		remove: func(string) error {
			t.Error("remove called after a failed backup check")
			return nil
		},
	}

	var restoreErr *RestoreError
	if err := restoreBackup(ops, rec); !errors.As(err, &restoreErr) || !errors.Is(err, statErr) {
		t.Fatalf("restoreBackup = %v, want a *RestoreError carrying %v", err, statErr)
	}
}

func TestRestoreInPlaceRenameFailureIsARestoreError(t *testing.T) {
	t.Parallel()

	rec := binaryRecord(t, t.TempDir(), true)
	boom := errors.New("rename refused")

	ops := osFSOps()
	ops.rename = func(string, string) error { return boom }

	err := restoreInPlace(ops, rec)

	var restoreErr *RestoreError
	if !errors.As(err, &restoreErr) {
		t.Fatalf("restoreInPlace = %v, want a *RestoreError", err)
	}

	if !errors.Is(err, boom) || restoreErr.Backup != rec.Backup || restoreErr.Target != rec.Target {
		t.Errorf("RestoreError = %+v, want the paths and %v", restoreErr, boom)
	}
}

func TestRestoreAsideTargetLstatFailureRenamesNothing(t *testing.T) {
	t.Parallel()

	rec := binaryRecord(t, t.TempDir(), true)
	denied := fs.ErrPermission

	ops := osFSOps()
	ops.lstat = func(string) (fs.FileInfo, error) { return nil, denied }
	ops.rename = func(oldpath, newpath string) error {
		t.Errorf("rename(%s, %s) called, want no rename", oldpath, newpath)

		return nil
	}

	err := restoreAside(ops, rec)

	var restoreErr *RestoreError
	if !errors.As(err, &restoreErr) || !errors.Is(err, denied) {
		t.Fatalf("restoreAside = %v, want a *RestoreError wrapping %v", err, denied)
	}

	if got := readRaw(t, rec.Target); got != "new" {
		t.Errorf("target = %q, want it untouched", got)
	}

	if got := readRaw(t, rec.Backup); got != "old" {
		t.Errorf("backup = %q, want it untouched", got)
	}
}

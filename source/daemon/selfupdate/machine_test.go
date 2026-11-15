package selfupdate

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/cli/state"
)

const (
	oldVersion = "v1.0.0"
	newVersion = "v1.1.0"
)

// fakeClock reads a fixed time. No Machine arms a timer, so AfterFunc never fires.
type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time { return c.now }

func (fakeClock) AfterFunc(time.Duration, func()) func() bool {
	return func() bool { return false }
}

func testTime() time.Time {
	return time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC)
}

// fakeStore is an in-memory Store whose Update fails with updateErr when it is set.
type fakeStore struct {
	mu        sync.Mutex
	rec       Record
	loadErr   error
	updateErr error
}

func (s *fakeStore) Load(context.Context) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.rec, s.loadErr
}

func (s *fakeStore) Update(_ context.Context, fn func(Record) (Record, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loadErr != nil {
		return s.loadErr
	}

	next, err := fn(s.rec)
	if err != nil {
		return err
	}

	if s.updateErr != nil {
		return s.updateErr
	}

	s.rec = next

	return nil
}

// interleavedStore is a fakeStore that applies between to the record before each Update after the
// first, standing for another caller that changes the record in the gap between two Updates.
type interleavedStore struct {
	fakeStore

	between func(*Record)
	updates int
}

func (s *interleavedStore) Update(ctx context.Context, fn func(Record) (Record, error)) error {
	s.mu.Lock()
	s.updates++
	if s.updates > 1 && s.between != nil {
		s.between(&s.rec)
	}
	s.mu.Unlock()

	return s.fakeStore.Update(ctx, fn)
}

// fileOps records the calls a Machine makes through its restore and removeFile seams.
type fileOps struct {
	mu         sync.Mutex
	restores   []Record
	removed    []string
	restoreErr error
	removeErr  error
}

func (f *fileOps) restore(rec Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.restores = append(f.restores, rec)

	return f.restoreErr
}

func (f *fileOps) remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.removed = append(f.removed, path)

	return f.removeErr
}

func (f *fileOps) restoreCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.restores)
}

func (f *fileOps) removedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.removed...)
}

// newMachine returns a Machine running version over store, with its file operations recorded
// by the returned fileOps.
func newMachine(t *testing.T, store Store, version string, opts ...Option) (*Machine, *fileOps) {
	t.Helper()

	opts = append([]Option{WithClock(fakeClock{now: testTime()})}, opts...)

	m, err := New(store, version, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ops := &fileOps{}
	m.restore = ops.restore
	m.removeFile = ops.remove

	return m, ops
}

func pendingRecord() Record {
	return Record{
		Status:      StatusPending,
		FromVersion: oldVersion,
		ToVersion:   newVersion,
		Target:      "/opt/tool/bin/tool",
		Backup:      "/opt/tool/bin/tool.old",
		StartedAt:   testTime().Add(-time.Hour),
		UpdatedAt:   testTime().Add(-time.Hour),
	}
}

// storeWith returns a FileStore in a temporary directory holding rec.
func storeWith(t *testing.T, rec Record) *FileStore {
	t.Helper()

	store := NewFileStore(recordPath(t))
	if err := store.Update(t.Context(), put(rec)); err != nil {
		t.Fatalf("store record: %v", err)
	}

	return store
}

func load(t *testing.T, store Store) Record {
	t.Helper()

	rec, err := store.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return rec
}

func TestNewRefusesInvalidArguments(t *testing.T) {
	t.Parallel()

	store := NewFileStore(recordPath(t))

	for name, build := range map[string]func() (*Machine, error){
		"nil store": func() (*Machine, error) { return New(nil, newVersion) },
		"nil clock": func() (*Machine, error) { return New(store, newVersion, WithClock(nil)) },
		"zero attempts": func() (*Machine, error) {
			return New(store, newVersion, WithMaxBootAttempts(0))
		},
		"negative attempts": func() (*Machine, error) {
			return New(store, newVersion, WithMaxBootAttempts(-1))
		},
	} {
		if m, err := build(); err == nil || m != nil {
			t.Errorf("%s: New = %v, %v, want nil and an error", name, m, err)
		}
	}
}

func TestNewDefaults(t *testing.T) {
	t.Parallel()

	m, err := New(NewFileStore(recordPath(t)), newVersion)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if m.maxBootAttempts != 3 {
		t.Errorf("maxBootAttempts = %d, want 3", m.maxBootAttempts)
	}

	if _, ok := m.clock.(systemClock); !ok {
		t.Errorf("clock = %T, want the system clock", m.clock)
	}
}

func TestBeginRecordsPending(t *testing.T) {
	t.Parallel()

	for _, prior := range []Record{
		{},
		{Status: StatusConfirmed, ToVersion: oldVersion, BootAttempts: 2, Reason: "x"},
		{Status: StatusRolledBack, ToVersion: oldVersion, BootAttempts: 4, Reason: "too many boots"},
	} {
		t.Run(prior.Status.String(), func(t *testing.T) {
			t.Parallel()

			store := storeWith(t, prior)
			m, _ := newMachine(t, store, oldVersion)

			p := Pending{FromVersion: oldVersion, ToVersion: newVersion, Target: "/t", Backup: "/t.old"}
			if err := m.Begin(t.Context(), p); err != nil {
				t.Fatalf("Begin: %v", err)
			}

			got := load(t, store)
			want := Record{
				Status:      StatusPending,
				FromVersion: oldVersion,
				ToVersion:   newVersion,
				Target:      "/t",
				Backup:      "/t.old",
				StartedAt:   testTime(),
				UpdatedAt:   testTime(),
			}

			if !got.StartedAt.Equal(want.StartedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
				t.Errorf("times = %v, %v, want %v", got.StartedAt, got.UpdatedAt, testTime())
			}

			got.StartedAt, got.UpdatedAt = want.StartedAt, want.UpdatedAt
			if got != want {
				t.Errorf("record = %+v, want %+v", got, want)
			}
		})
	}
}

func TestBeginRefusedWhilePending(t *testing.T) {
	t.Parallel()

	store := storeWith(t, pendingRecord())
	m, _ := newMachine(t, store, newVersion)

	err := m.Begin(t.Context(), Pending{ToVersion: "v1.2.0", Target: "/t", Backup: "/t.old"})
	if !errors.Is(err, ErrUpdatePending) {
		t.Errorf("Begin error = %v, want ErrUpdatePending", err)
	}

	if got := load(t, store); got.ToVersion != newVersion || got.Status != StatusPending {
		t.Errorf("record = %+v, want the pending update untouched", got)
	}
}

func TestBeginValidatesFields(t *testing.T) {
	t.Parallel()

	full := Pending{FromVersion: oldVersion, ToVersion: newVersion, Target: "/t", Backup: "/t.old"}

	for name, edit := range map[string]func(*Pending){
		"ToVersion": func(p *Pending) { p.ToVersion = "" },
		"Target":    func(p *Pending) { p.Target = "" },
		"Backup":    func(p *Pending) { p.Backup = "" },
	} {
		path := recordPath(t)
		m, _ := newMachine(t, NewFileStore(path), oldVersion)

		p := full
		edit(&p)

		if err := m.Begin(t.Context(), p); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("Begin without %s: error = %v, want one naming it", name, err)
		}

		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Begin without %s wrote a record: %v", name, err)
		}
	}
}

func TestBootCountsAttemptsAndRollsBackPastTheLimit(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		opts  []Option
		limit int
	}{
		"default": {limit: 3},
		"limit 1": {opts: []Option{WithMaxBootAttempts(1)}, limit: 1},
		"limit 5": {opts: []Option{WithMaxBootAttempts(5)}, limit: 5},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := storeWith(t, pendingRecord())
			m, ops := newMachine(t, store, newVersion, tc.opts...)

			for boot := 1; boot <= tc.limit; boot++ {
				rolledBack, err := m.Boot(t.Context())
				if err != nil || rolledBack {
					t.Fatalf("boot %d: Boot = %v, %v, want false, nil", boot, rolledBack, err)
				}

				got := load(t, store)
				if got.Status != StatusPending || got.BootAttempts != boot {
					t.Fatalf("after boot %d: record = %+v, want pending with %d attempts", boot, got, boot)
				}
			}

			if n := ops.restoreCount(); n != 0 {
				t.Fatalf("restore called %d times within the limit, want 0", n)
			}

			rolledBack, err := m.Boot(t.Context())
			if err != nil || !rolledBack {
				t.Fatalf("boot %d: Boot = %v, %v, want true, nil", tc.limit+1, rolledBack, err)
			}

			if n := ops.restoreCount(); n != 1 {
				t.Errorf("restore called %d times, want 1", n)
			}

			got := load(t, store)
			if got.Status != StatusRolledBack || got.BootAttempts != tc.limit+1 || got.Reason == "" {
				t.Errorf("record = %+v, want rolled_back with %d attempts and a reason", got, tc.limit+1)
			}

			if !got.UpdatedAt.Equal(testTime()) {
				t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, testTime())
			}
		})
	}
}

func TestBootSavesTheAttemptBeforeJudging(t *testing.T) {
	t.Parallel()

	errSave := errors.New("disk full")
	rec := pendingRecord()
	rec.BootAttempts = 3
	store := &fakeStore{rec: rec, updateErr: errSave}
	m, ops := newMachine(t, store, newVersion)

	rolledBack, err := m.Boot(t.Context())
	if !errors.Is(err, errSave) || rolledBack {
		t.Errorf("Boot = %v, %v, want false and the save error", rolledBack, err)
	}

	if n := ops.restoreCount(); n != 0 {
		t.Errorf("restore called %d times after a failed save, want 0", n)
	}

	if got := load(t, store); got != rec {
		t.Errorf("record = %+v, want it unchanged: %+v", got, rec)
	}
}

func TestBootPastTheLimitAfterAnotherRollbackReportsRolledBack(t *testing.T) {
	t.Parallel()

	rec := pendingRecord()
	rec.BootAttempts = 3

	rolledBackElsewhere := rec
	rolledBackElsewhere.BootAttempts = 4
	rolledBackElsewhere.Status = StatusRolledBack
	rolledBackElsewhere.Reason = "health check failed"

	store := &interleavedStore{
		fakeStore: fakeStore{rec: rec},
		between:   func(r *Record) { *r = rolledBackElsewhere },
	}
	m, ops := newMachine(t, store, newVersion)

	rolledBack, err := m.Boot(t.Context())
	if err != nil || !rolledBack {
		t.Fatalf("Boot = %v, %v, want true, nil", rolledBack, err)
	}

	if n := ops.restoreCount(); n != 0 {
		t.Errorf("restore called %d times, want 0", n)
	}

	if got := load(t, store); got != rolledBackElsewhere {
		t.Errorf("record = %+v, want the other rollback unchanged: %+v", got, rolledBackElsewhere)
	}
}

func TestBootOfAnotherVersionRecordsRolledBackWithoutFiles(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		running string
		reason  string
	}{
		"previous build running": {running: oldVersion, reason: "did not install"},
		"other build running":    {running: "v9.0.0", reason: "replaced outside"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := pendingRecord()
			rec.BootAttempts = 2
			store := storeWith(t, rec)
			m, ops := newMachine(t, store, tc.running)

			rolledBack, err := m.Boot(t.Context())
			if err != nil || rolledBack {
				t.Fatalf("Boot = %v, %v, want false, nil", rolledBack, err)
			}

			got := load(t, store)
			if got.Status != StatusRolledBack || !strings.Contains(got.Reason, tc.reason) {
				t.Errorf("record = %+v, want rolled_back with a reason containing %q", got, tc.reason)
			}

			if got.BootAttempts != 2 {
				t.Errorf("BootAttempts = %d, want 2: no attempt counted", got.BootAttempts)
			}

			if n, removed := ops.restoreCount(), ops.removedPaths(); n != 0 || len(removed) != 0 {
				t.Errorf("file operations: %d restores, removed %v, want none", n, removed)
			}
		})
	}
}

func TestBootWhenNotPendingRemovesTheFailedBinary(t *testing.T) {
	t.Parallel()

	for _, rec := range []Record{
		{Status: StatusConfirmed, Target: "/opt/tool/bin/tool"},
		{Status: StatusRolledBack, Target: "/opt/tool/bin/tool"},
	} {
		store := storeWith(t, rec)
		m, ops := newMachine(t, store, oldVersion)
		ops.removeErr = errors.New("permission denied")

		rolledBack, err := m.Boot(t.Context())
		if err != nil || rolledBack {
			t.Errorf("%v: Boot = %v, %v, want false, nil despite the removal error", rec.Status, rolledBack, err)
		}

		if got, want := ops.removedPaths(), []string{"/opt/tool/bin/tool.failed"}; len(got) != 1 || got[0] != want[0] {
			t.Errorf("%v: removed %v, want %v", rec.Status, got, want)
		}

		if got := load(t, store); got != rec {
			t.Errorf("%v: record = %+v, want it unchanged", rec.Status, got)
		}
	}
}

func TestBootWhenIdleTouchesNothing(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	m, ops := newMachine(t, NewFileStore(path), oldVersion)

	rolledBack, err := m.Boot(t.Context())
	if err != nil || rolledBack {
		t.Errorf("Boot = %v, %v, want false, nil", rolledBack, err)
	}

	if removed := ops.removedPaths(); len(removed) != 0 {
		t.Errorf("removed %v with no target recorded, want nothing", removed)
	}

	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Boot wrote a record while idle: %v", err)
	}
}

func TestBootOnACorruptRecordIsAnError(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	writeRaw(t, path, "{garbage")
	m, ops := newMachine(t, NewFileStore(path), newVersion)

	rolledBack, err := m.Boot(t.Context())
	if !errors.Is(err, state.ErrCorrupt) || rolledBack {
		t.Errorf("Boot = %v, %v, want false and an error matching state.ErrCorrupt", rolledBack, err)
	}

	if n, removed := ops.restoreCount(), ops.removedPaths(); n != 0 || len(removed) != 0 {
		t.Errorf("file operations: %d restores, removed %v, want none", n, removed)
	}
}

func TestRestoreFailureLeavesThePendingRecord(t *testing.T) {
	t.Parallel()

	for name, restoreErr := range map[string]error{
		"no backup":      &NoBackupError{Path: "/opt/tool/bin/tool.old"},
		"rename failure": &RestoreError{Backup: "b", Target: "t", Err: fs.ErrPermission},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := pendingRecord()
			rec.BootAttempts = 3
			store := storeWith(t, rec)
			m, ops := newMachine(t, store, newVersion)
			ops.restoreErr = restoreErr

			rolledBack, err := m.Boot(t.Context())
			if !errors.Is(err, restoreErr) || rolledBack {
				t.Errorf("Boot = %v, %v, want false and the restore error", rolledBack, err)
			}

			got := load(t, store)
			if got.Status != StatusPending || got.BootAttempts != 4 {
				t.Errorf("record = %+v, want pending with the attempt counted", got)
			}

			rolledBack, err = m.MarkFailed(t.Context(), "health check failed")
			if !errors.Is(err, restoreErr) || rolledBack {
				t.Errorf("MarkFailed = %v, %v, want false and the restore error", rolledBack, err)
			}

			if got := load(t, store); got.Status != StatusPending {
				t.Errorf("after MarkFailed: status = %v, want pending", got.Status)
			}
		})
	}
}

func TestRollbackWithTheDefaultRestoreAndNoBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := pendingRecord()
	rec.Target = filepath.Join(dir, "tool")
	rec.Backup = filepath.Join(dir, "tool.old")
	writeRaw(t, rec.Target, "new")

	store := storeWith(t, rec)

	m, err := New(store, newVersion, WithClock(fakeClock{now: testTime()}))
	if err != nil {
		t.Fatal(err)
	}

	rolledBack, err := m.MarkFailed(t.Context(), "broken")

	var noBackup *NoBackupError
	if !errors.As(err, &noBackup) || noBackup.Path != rec.Backup || rolledBack {
		t.Errorf("MarkFailed = %v, %v, want false and a *NoBackupError for %s", rolledBack, err, rec.Backup)
	}

	if got := readRaw(t, rec.Target); got != "new" {
		t.Errorf("target = %q, want it untouched", got)
	}

	if got := load(t, store); got.Status != StatusPending {
		t.Errorf("status = %v, want pending", got.Status)
	}
}

func TestRollbackSaveFailureReportsTheRestore(t *testing.T) {
	t.Parallel()

	errSave := errors.New("disk full")
	store := &fakeStore{rec: pendingRecord(), updateErr: errSave}
	m, ops := newMachine(t, store, newVersion)

	rolledBack, err := m.MarkFailed(t.Context(), "broken")
	if !errors.Is(err, errSave) || !rolledBack {
		t.Errorf("MarkFailed = %v, %v, want true and the save error", rolledBack, err)
	}

	if n := ops.restoreCount(); n != 1 {
		t.Errorf("restore called %d times, want 1", n)
	}
}

func TestMarkFailed(t *testing.T) {
	t.Parallel()

	t.Run("rolls back at once", func(t *testing.T) {
		t.Parallel()

		store := storeWith(t, pendingRecord())
		m, ops := newMachine(t, store, newVersion)

		rolledBack, err := m.MarkFailed(t.Context(), "health check failed")
		if err != nil || !rolledBack {
			t.Fatalf("MarkFailed = %v, %v, want true, nil", rolledBack, err)
		}

		if n := ops.restoreCount(); n != 1 {
			t.Errorf("restore called %d times, want 1", n)
		}

		got := load(t, store)
		if got.Status != StatusRolledBack || got.Reason != "health check failed" || got.BootAttempts != 0 {
			t.Errorf("record = %+v, want rolled_back with the reason and no attempt counted", got)
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		t.Parallel()

		store := storeWith(t, pendingRecord())
		m, ops := newMachine(t, store, oldVersion)

		rolledBack, err := m.MarkFailed(t.Context(), "broken")
		if !errors.Is(err, ErrNotUpdatedBuild) || rolledBack {
			t.Errorf("MarkFailed = %v, %v, want false, ErrNotUpdatedBuild", rolledBack, err)
		}

		if n := ops.restoreCount(); n != 0 {
			t.Errorf("restore called %d times, want 0", n)
		}

		if got := load(t, store); got.Status != StatusPending {
			t.Errorf("status = %v, want pending", got.Status)
		}
	})

	t.Run("already rolled back for the running version", func(t *testing.T) {
		t.Parallel()

		rec := pendingRecord()
		rec.Status = StatusRolledBack
		rec.Reason = "boot attempt 4 of v1.1.0 exceeds the limit of 3"
		store := storeWith(t, rec)
		m, ops := newMachine(t, store, newVersion)

		rolledBack, err := m.MarkFailed(t.Context(), "broken")
		if err != nil || !rolledBack {
			t.Fatalf("MarkFailed = %v, %v, want true, nil", rolledBack, err)
		}

		if n := ops.restoreCount(); n != 0 {
			t.Errorf("restore called %d times, want 0", n)
		}

		if got := load(t, store); got != rec {
			t.Errorf("record = %+v, want it unchanged: %+v", got, rec)
		}
	})

	t.Run("rolled back for another version", func(t *testing.T) {
		t.Parallel()

		rec := pendingRecord()
		rec.Status = StatusRolledBack
		rec.Reason = "health check failed"
		store := storeWith(t, rec)
		m, ops := newMachine(t, store, oldVersion)

		rolledBack, err := m.MarkFailed(t.Context(), "broken")
		if err != nil || rolledBack {
			t.Errorf("MarkFailed = %v, %v, want false, nil", rolledBack, err)
		}

		if n := ops.restoreCount(); n != 0 {
			t.Errorf("restore called %d times, want 0", n)
		}

		if got := load(t, store); got != rec {
			t.Errorf("record = %+v, want it unchanged: %+v", got, rec)
		}
	})

	t.Run("not pending", func(t *testing.T) {
		t.Parallel()

		for _, rec := range []Record{{}, {Status: StatusConfirmed}, {Status: StatusRolledBack}} {
			store := storeWith(t, rec)
			m, ops := newMachine(t, store, newVersion)

			rolledBack, err := m.MarkFailed(t.Context(), "broken")
			if err != nil || rolledBack {
				t.Errorf("%v: MarkFailed = %v, %v, want false, nil", rec.Status, rolledBack, err)
			}

			if n := ops.restoreCount(); n != 0 {
				t.Errorf("%v: restore called %d times, want 0", rec.Status, n)
			}

			if got := load(t, store); got != rec {
				t.Errorf("%v: record = %+v, want it unchanged", rec.Status, got)
			}
		}
	})
}

func TestConfirm(t *testing.T) {
	t.Parallel()

	t.Run("not pending", func(t *testing.T) {
		t.Parallel()

		for _, rec := range []Record{{}, {Status: StatusConfirmed, Backup: "/b"}, {Status: StatusRolledBack, Backup: "/b"}} {
			store := storeWith(t, rec)
			m, ops := newMachine(t, store, newVersion)

			if err := m.Confirm(t.Context()); err != nil {
				t.Errorf("%v: Confirm = %v, want nil", rec.Status, err)
			}

			if removed := ops.removedPaths(); len(removed) != 0 {
				t.Errorf("%v: removed %v, want nothing", rec.Status, removed)
			}

			if got := load(t, store); got != rec {
				t.Errorf("%v: record = %+v, want it unchanged", rec.Status, got)
			}
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		t.Parallel()

		store := storeWith(t, pendingRecord())
		m, ops := newMachine(t, store, oldVersion)

		if err := m.Confirm(t.Context()); !errors.Is(err, ErrNotUpdatedBuild) {
			t.Errorf("Confirm = %v, want ErrNotUpdatedBuild", err)
		}

		if removed := ops.removedPaths(); len(removed) != 0 {
			t.Errorf("removed %v, want nothing", removed)
		}

		if got := load(t, store); got.Status != StatusPending {
			t.Errorf("status = %v, want pending", got.Status)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		store := storeWith(t, pendingRecord())
		m, ops := newMachine(t, store, newVersion)

		if err := m.Confirm(t.Context()); err != nil {
			t.Fatalf("Confirm = %v", err)
		}

		got := load(t, store)
		if got.Status != StatusConfirmed || !got.UpdatedAt.Equal(testTime()) {
			t.Errorf("record = %+v, want confirmed at %v", got, testTime())
		}

		if removed := ops.removedPaths(); len(removed) != 1 || removed[0] != pendingRecord().Backup {
			t.Errorf("removed %v, want the backup %s", removed, pendingRecord().Backup)
		}
	})

	t.Run("backup already gone", func(t *testing.T) {
		t.Parallel()

		store := storeWith(t, pendingRecord())
		m, ops := newMachine(t, store, newVersion)
		ops.removeErr = &fs.PathError{Op: "remove", Path: pendingRecord().Backup, Err: fs.ErrNotExist}

		if err := m.Confirm(t.Context()); err != nil {
			t.Errorf("Confirm = %v, want nil", err)
		}

		if got := load(t, store); got.Status != StatusConfirmed {
			t.Errorf("status = %v, want confirmed", got.Status)
		}
	})

	t.Run("removal failure stays confirmed", func(t *testing.T) {
		t.Parallel()

		store := storeWith(t, pendingRecord())
		m, ops := newMachine(t, store, newVersion)
		ops.removeErr = fs.ErrPermission

		err := m.Confirm(t.Context())

		var removal *BackupRemovalError
		if !errors.As(err, &removal) || removal.Path != pendingRecord().Backup || !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Confirm = %v, want a *BackupRemovalError for %s", err, pendingRecord().Backup)
		}

		if got := load(t, store); got.Status != StatusConfirmed {
			t.Errorf("status = %v, want confirmed", got.Status)
		}
	})
}

func TestBackupInUse(t *testing.T) {
	t.Parallel()

	for status, want := range map[Status]bool{
		StatusIdle:       false,
		StatusPending:    true,
		StatusConfirmed:  false,
		StatusRolledBack: false,
	} {
		rec := pendingRecord()
		rec.Status = status
		m, _ := newMachine(t, storeWith(t, rec), newVersion)

		got, err := m.BackupInUse(t.Context())
		if err != nil || got != want {
			t.Errorf("%v: BackupInUse = %v, %v, want %v, nil", status, got, err, want)
		}
	}
}

func TestBackupInUseOnACorruptRecordIsAnError(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	writeRaw(t, path, "not json")
	m, _ := newMachine(t, NewFileStore(path), newVersion)

	if _, err := m.BackupInUse(t.Context()); !errors.Is(err, state.ErrCorrupt) {
		t.Errorf("BackupInUse error = %v, want one matching state.ErrCorrupt", err)
	}
}

func TestState(t *testing.T) {
	t.Parallel()

	rec := pendingRecord()
	m, _ := newMachine(t, &fakeStore{rec: rec}, newVersion)

	got, err := m.State(t.Context())
	if err != nil || got != rec {
		t.Errorf("State = %+v, %v, want %+v, nil", got, err, rec)
	}

	errLoad := errors.New("unreadable")
	m, _ = newMachine(t, &fakeStore{loadErr: errLoad}, newVersion)

	if _, err := m.State(t.Context()); !errors.Is(err, errLoad) {
		t.Errorf("State error = %v, want the load error", err)
	}
}

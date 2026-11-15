package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/cli/state"
)

func recordPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "selfupdate.json")
}

func writeRaw(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRaw(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func put(rec Record) func(Record) (Record, error) {
	return func(Record) (Record, error) { return rec, nil }
}

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := NewFileStore(recordPath(t))
	started := time.Date(2026, 3, 4, 5, 6, 7, 0, time.FixedZone("x", 3600))
	want := Record{
		Status:       StatusPending,
		FromVersion:  "v1.0.0",
		ToVersion:    "v1.1.0",
		Target:       "/usr/local/bin/tool",
		Backup:       "/usr/local/bin/tool.old",
		BootAttempts: 2,
		Reason:       "boot",
		StartedAt:    started,
		UpdatedAt:    started.Add(time.Minute),
	}

	if err := store.Update(ctx, put(want)); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !got.StartedAt.Equal(want.StartedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("times = %v, %v, want %v, %v", got.StartedAt, got.UpdatedAt, want.StartedAt, want.UpdatedAt)
	}

	if got.StartedAt.Location() != time.UTC {
		t.Errorf("StartedAt location = %v, want UTC", got.StartedAt.Location())
	}

	got.StartedAt, got.UpdatedAt = want.StartedAt, want.UpdatedAt
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestEveryStatusRoundTrips(t *testing.T) {
	t.Parallel()

	for _, st := range []Status{StatusIdle, StatusPending, StatusConfirmed, StatusRolledBack} {
		store := NewFileStore(recordPath(t))
		if err := store.Update(t.Context(), put(Record{Status: st})); err != nil {
			t.Fatalf("Update(%v): %v", st, err)
		}

		got, err := store.Load(t.Context())
		if err != nil {
			t.Fatalf("Load(%v): %v", st, err)
		}

		if got.Status != st {
			t.Errorf("Status = %v, want %v", got.Status, st)
		}
	}
}

func TestZeroRecordIsStoredAsIdle(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(Record{})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(data), `{"status":"idle"}`; got != want {
		t.Errorf("zero Record = %s, want %s", got, want)
	}

	if got := StatusIdle.String(); got != "idle" {
		t.Errorf("StatusIdle.String() = %q, want idle", got)
	}
}

func TestUnknownStatusDoesNotMarshal(t *testing.T) {
	t.Parallel()

	if _, err := json.Marshal(Record{Status: "paused"}); err == nil {
		t.Error("Marshal of an unknown status succeeded, want an error")
	}
}

func TestFileStoreMissingFileIsIdle(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	store := NewFileStore(path)

	got, err := store.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got != (Record{}) {
		t.Errorf("Load = %+v, want the zero Record", got)
	}

	var seen *Record

	err = store.Update(t.Context(), func(rec Record) (Record, error) {
		seen = &rec
		return rec, nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if seen == nil || *seen != (Record{}) {
		t.Errorf("Update passed %+v, want the zero Record", seen)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("Update did not store the record: %v", err)
	}
}

func TestFileStoreCorruptRecordIsAnError(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"empty":          "",
		"malformed":      "{not json",
		"unknown status": `{"status":"paused"}`,
		"wrong type":     `{"status":"pending","boot_attempts":"two"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := recordPath(t)
			writeRaw(t, path, content)
			store := NewFileStore(path)

			if _, err := store.Load(t.Context()); !errors.Is(err, state.ErrCorrupt) {
				t.Errorf("Load error = %v, want one matching state.ErrCorrupt", err)
			}

			called := false
			err := store.Update(t.Context(), func(rec Record) (Record, error) {
				called = true
				return rec, nil
			})

			if !errors.Is(err, state.ErrCorrupt) {
				t.Errorf("Update error = %v, want one matching state.ErrCorrupt", err)
			}

			if called {
				t.Error("Update called fn on a corrupt record")
			}

			if got := readRaw(t, path); got != content {
				t.Errorf("file = %q, want it untouched: %q", got, content)
			}
		})
	}
}

func TestFileStoreUnreadableRecordIsAnError(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	store := NewFileStore(path)

	_, err := store.Load(t.Context())
	if err == nil || errors.Is(err, state.ErrCorrupt) {
		t.Errorf("Load error = %v, want a read error", err)
	}

	called := false
	err = store.Update(t.Context(), func(rec Record) (Record, error) {
		called = true
		return rec, nil
	})

	if err == nil || errors.Is(err, state.ErrCorrupt) {
		t.Errorf("Update error = %v, want a read error", err)
	}

	if called {
		t.Error("Update called fn on an unreadable record")
	}
}

func TestFileStoreCreatesNoDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "missing")
	store := NewFileStore(filepath.Join(dir, "selfupdate.json"))

	if err := store.Update(t.Context(), put(Record{Status: StatusPending})); err == nil {
		t.Error("Update into a missing directory succeeded, want an error")
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat(%s) error = %v, want the directory still missing", dir, err)
	}
}

func TestFileStoreFnErrorWritesNothing(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	store := NewFileStore(path)

	if err := store.Update(t.Context(), put(Record{Status: StatusPending, ToVersion: "v1.1.0"})); err != nil {
		t.Fatal(err)
	}

	before := readRaw(t, path)
	errRefused := errors.New("refused")

	err := store.Update(t.Context(), func(Record) (Record, error) {
		return Record{Status: StatusConfirmed}, errRefused
	})
	if !errors.Is(err, errRefused) {
		t.Errorf("Update error = %v, want fn's error", err)
	}

	if got := readRaw(t, path); got != before {
		t.Errorf("file = %q, want it unchanged: %q", got, before)
	}
}

func TestFileStoreDoneContext(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	store := NewFileStore(path)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := store.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Load error = %v, want context.Canceled", err)
	}

	called := false
	err := store.Update(ctx, func(rec Record) (Record, error) {
		called = true
		return rec, nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Update error = %v, want context.Canceled", err)
	}

	if called {
		t.Error("Update called fn with a done context")
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat error = %v, want no record written", err)
	}
}

func TestFileStoreLoadAbandonsAStalledRead(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	defer close(release)

	store := NewFileStore(recordPath(t))
	store.readFile = func() (Record, error) {
		<-release
		return Record{}, nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if _, err := store.Load(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Load error = %v, want context.DeadlineExceeded", err)
	}
}

func TestFileStoreAbandonedWriteHoldsTheLock(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	stalled := NewFileStore(path)
	writing := make(chan struct{})
	release := make(chan struct{})
	stalled.writeFile = func(rec Record) error {
		close(writing)
		<-release

		return stalled.write(rec)
	}

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)

	go func() {
		errc <- stalled.Update(ctx, put(Record{Status: StatusPending, BootAttempts: 1}))
	}()

	<-writing
	cancel()

	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Update error = %v, want context.Canceled", err)
	}

	other := NewFileStore(path)
	short, cancelShort := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelShort()

	if err := other.Update(short, put(Record{})); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Update during the abandoned write: error = %v, want context.DeadlineExceeded", err)
	}

	close(release)

	err := other.Update(t.Context(), func(rec Record) (Record, error) {
		if rec.BootAttempts != 1 {
			t.Errorf("Update after the abandoned write read %+v, want its record", rec)
		}

		return rec, nil
	})
	if err != nil {
		t.Fatalf("Update after the abandoned write: %v", err)
	}
}

func TestFileStoreUpdatePanicReleasesTheLock(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	store := NewFileStore(path)

	func() {
		defer func() {
			if recover() == nil {
				t.Error("Update did not propagate fn's panic")
			}
		}()

		_ = store.Update(t.Context(), func(Record) (Record, error) { panic("fn") })
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := NewFileStore(path).Update(ctx, put(Record{})); err != nil {
		t.Errorf("Update after a panic in fn: %v", err)
	}
}

func TestFileStoreUpdatesAreSerialised(t *testing.T) {
	t.Parallel()

	const perStore = 25

	path := recordPath(t)
	stores := []*FileStore{NewFileStore(path), NewFileStore(path)}

	var wg sync.WaitGroup

	errs := make(chan error, len(stores)*perStore)

	for _, store := range stores {
		wg.Go(func() {
			for range perStore {
				errs <- store.Update(t.Context(), func(rec Record) (Record, error) {
					rec.BootAttempts++
					return rec, nil
				})
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	got, err := stores[0].Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if want := len(stores) * perStore; got.BootAttempts != want {
		t.Errorf("BootAttempts = %d, want %d", got.BootAttempts, want)
	}
}

func TestFileStoreErrorsNameTheRecord(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	writeRaw(t, path, "{")

	_, err := NewFileStore(path).Load(t.Context())
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Load error = %v, want it to name %s", err, path)
	}
}

package filelock_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/filelock"
)

// fakeCalls counts the attempts of a fake tryLock and records the descriptors it saw.
type fakeCalls struct {
	mu       sync.Mutex
	files    []*os.File
	attempts atomic.Int32
	unlocks  atomic.Int32
	grantAt  int32 // the attempt that is granted; 0 never grants
	err      error // returned by every attempt when non-nil
}

func (c *fakeCalls) tryLock(f *os.File) (bool, error) {
	c.mu.Lock()
	c.files = append(c.files, f)
	c.mu.Unlock()

	n := c.attempts.Add(1)
	if c.err != nil {
		return false, c.err
	}

	return c.grantAt != 0 && n >= c.grantAt, nil
}

func (c *fakeCalls) unlockFile(*os.File) error {
	c.unlocks.Add(1)

	return nil
}

func (c *fakeCalls) locker(path string) filelock.Locker {
	return filelock.NewWith(path, filelock.WithLockCalls(c.tryLock, c.unlockFile))
}

func (c *fakeCalls) lastFile(t *testing.T) *os.File {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.files) == 0 {
		t.Fatal("tryLock was never called")
	}

	return c.files[len(c.files)-1]
}

func assertClosed(t *testing.T, f *os.File) {
	t.Helper()

	if _, err := f.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Stat on the lock descriptor = %v, want %v", err, os.ErrClosed)
	}
}

func assertPrefix(t *testing.T, err error, prefix string) {
	t.Helper()

	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		t.Errorf("error = %v, want prefix %q", err, prefix)
	}
}

func lockPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "lock")
}

func TestNoopGrantsRepeatedly(t *testing.T) {
	t.Parallel()

	l := filelock.NewNoop()

	for range 3 {
		unlock, err := l.Lock(t.Context())
		if err != nil {
			t.Fatalf("Lock: %v", err)
		}

		unlock()
		unlock()
	}
}

func TestNoopGrantsConcurrently(t *testing.T) {
	t.Parallel()

	l := filelock.NewNoop()

	first, err := l.Lock(t.Context())
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	defer first()

	second, err := l.Lock(t.Context())
	if err != nil {
		t.Fatalf("second Lock while the first is held: %v", err)
	}

	second()
}

func TestNoopFailsOnCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	unlock, err := filelock.NewNoop().Lock(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Lock error = %v, want %v", err, context.Canceled)
	}

	if unlock != nil {
		t.Error("Lock returned an unlock function with its error")
	}

	assertPrefix(t, err, "filelock: lock: ")
}

func TestLockAcquiresAfterWouldBlockRetries(t *testing.T) {
	t.Parallel()

	calls := &fakeCalls{grantAt: 4}
	path := lockPath(t)

	unlock, err := calls.locker(path).Lock(t.Context())
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer unlock()

	if got := calls.attempts.Load(); got != 4 {
		t.Errorf("tryLock attempts = %d, want 4", got)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file: %v", err)
	}
}

func TestLockCreatesFileOwnerOnly(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}

	calls := &fakeCalls{grantAt: 1}
	path := lockPath(t)

	unlock, err := calls.locker(path).Lock(t.Context())
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer unlock()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode of %s = %o, want %o", path, perm, 0o600)
	}
}

func TestLockHonoursDeadlineBetweenRetries(t *testing.T) {
	t.Parallel()

	calls := &fakeCalls{}
	path := lockPath(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*filelock.PollInterval)
	defer cancel()

	start := time.Now()

	unlock, err := calls.locker(path).Lock(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock error = %v, want %v", err, context.DeadlineExceeded)
	}

	if unlock != nil {
		t.Error("Lock returned an unlock function with its error")
	}

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Lock returned after %v, want close to the deadline", elapsed)
	}

	if got := calls.attempts.Load(); got < 2 {
		t.Errorf("tryLock attempts = %d, want at least 2", got)
	}

	assertPrefix(t, err, "filelock: lock "+path+": ")
	assertClosed(t, calls.lastFile(t))
}

func TestLockFailsOnCancelledContextWhenFree(t *testing.T) {
	t.Parallel()

	calls := &fakeCalls{grantAt: 1}
	path := lockPath(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := calls.locker(path).Lock(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Lock error = %v, want %v", err, context.Canceled)
	}

	assertPrefix(t, err, "filelock: lock "+path+": ")

	if got := calls.attempts.Load(); got != 0 {
		t.Errorf("tryLock attempts = %d, want 0", got)
	}

	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock file stat = %v, want it not created", err)
	}
}

func TestLockWrapsTryLockError(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")
	calls := &fakeCalls{err: cause}
	path := lockPath(t)

	_, err := calls.locker(path).Lock(t.Context())
	if !errors.Is(err, cause) {
		t.Fatalf("Lock error = %v, want it to wrap %v", err, cause)
	}

	if want := "filelock: lock " + path + ": boom"; err.Error() != want {
		t.Errorf("Lock error = %q, want %q", err, want)
	}

	if got := calls.attempts.Load(); got != 1 {
		t.Errorf("tryLock attempts = %d, want 1", got)
	}

	assertClosed(t, calls.lastFile(t))
}

func TestLockMissingDirectoryIsWrappedError(t *testing.T) {
	t.Parallel()

	calls := &fakeCalls{grantAt: 1}
	path := filepath.Join(t.TempDir(), "missing", "lock")

	_, err := calls.locker(path).Lock(t.Context())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Lock error = %v, want it to wrap %v", err, fs.ErrNotExist)
	}

	assertPrefix(t, err, "filelock: open "+path+": ")

	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("directory stat = %v, want it not created", err)
	}
}

func TestUnlockIsIdempotentAndClosesFile(t *testing.T) {
	t.Parallel()

	calls := &fakeCalls{grantAt: 1}
	path := lockPath(t)

	unlock, err := calls.locker(path).Lock(t.Context())
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	unlock()
	unlock()

	if got := calls.unlocks.Load(); got != 1 {
		t.Errorf("unlockFile calls = %d, want 1", got)
	}

	assertClosed(t, calls.lastFile(t))

	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file after unlock: %v, want it kept", err)
	}
}

func TestEachLockOpensItsOwnDescriptor(t *testing.T) {
	t.Parallel()

	calls := &fakeCalls{grantAt: 1}
	l := calls.locker(lockPath(t))

	first, err := l.Lock(t.Context())
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	defer first()

	second, err := l.Lock(t.Context())
	if err != nil {
		t.Fatalf("second Lock: %v", err)
	}
	defer second()

	calls.mu.Lock()
	defer calls.mu.Unlock()

	if len(calls.files) != 2 || calls.files[0] == calls.files[1] {
		t.Errorf("descriptors = %v, want two distinct ones", calls.files)
	}
}

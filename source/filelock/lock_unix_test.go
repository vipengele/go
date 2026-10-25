//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package filelock_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"go.vipengele.org/filelock"
)

// waitTimeout bounds every wait for a lock that is expected to stay held. It spans many poll
// intervals so a slow runner under -race still makes several attempts before the deadline.
const waitTimeout = 20 * filelock.PollInterval

// lockOrFatal takes l with ctx and fails the test when it cannot.
func lockOrFatal(t *testing.T, ctx context.Context, l filelock.Locker) func() {
	t.Helper()

	unlock, err := l.Lock(ctx)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	return unlock
}

// assertHeld fails the test unless l cannot take its lock within waitTimeout.
func assertHeld(t *testing.T, l filelock.Locker) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()

	unlock, err := l.Lock(ctx)
	if err == nil {
		unlock()
		t.Fatal("Lock succeeded while another Locker holds the lock")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestFlockExcludesASecondLocker(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	first := filelock.New(path)
	second := filelock.New(path)

	unlock := lockOrFatal(t, t.Context(), first)

	assertHeld(t, second)

	unlock()

	lockOrFatal(t, t.Context(), second)()
}

func TestFlockWaiterAcquiresWhenHolderUnlocks(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	unlock := lockOrFatal(t, t.Context(), filelock.New(path))

	acquired := make(chan error, 1)

	go func() {
		release, err := filelock.New(path).Lock(t.Context())
		if err == nil {
			release()
		}
		acquired <- err
	}()

	unlock()

	if err := <-acquired; err != nil {
		t.Fatalf("waiting Lock: %v", err)
	}
}

func TestFlockIsReusableAfterUnlock(t *testing.T) {
	t.Parallel()

	l := filelock.New(lockPath(t))

	for range 5 {
		unlock := lockOrFatal(t, t.Context(), l)
		unlock()
	}
}

func TestFlockTimeoutWrapsDeadlineExceeded(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	unlock := lockOrFatal(t, t.Context(), filelock.New(path))
	defer unlock()

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()

	release, err := filelock.New(path).Lock(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock error = %v, want %v", err, context.DeadlineExceeded)
	}

	if release != nil {
		t.Error("Lock returned an unlock function with its error")
	}

	assertPrefix(t, err, "filelock: lock "+path+": ")
}

func TestFlockCancelledContextFailsOnFreeLock(t *testing.T) {
	t.Parallel()

	path := lockPath(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := filelock.New(path).Lock(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Lock error = %v, want %v", err, context.Canceled)
	}

	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock file stat = %v, want it not created", err)
	}
}

func TestFlockCreatesFileOwnerOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A file created at 0777 keeps exactly the bits the process umask allows.
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o777); err != nil {
		t.Fatal(err)
	}

	probeInfo, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}

	if probeInfo.Mode().Perm()&0o600 != 0o600 {
		t.Skipf("umask removes owner bits (probe mode %o)", probeInfo.Mode().Perm())
	}

	path := filepath.Join(dir, "lock")
	lockOrFatal(t, t.Context(), filelock.New(path))()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	perm := info.Mode().Perm()
	if perm&0o600 != 0o600 || perm&0o077 != 0 {
		t.Errorf("mode of %s = %o, want owner read-write and no group or other bits", path, perm)
	}
}

func TestFlockAcquiresLeftoverFileWithNoHolder(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	if err := os.WriteFile(path, []byte("left behind"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()

	lockOrFatal(t, ctx, filelock.New(path))()
}

func TestFlockRepeatedUnlockKeepsLaterHolder(t *testing.T) {
	t.Parallel()

	path := lockPath(t)

	stale := lockOrFatal(t, t.Context(), filelock.New(path))
	stale()

	current := lockOrFatal(t, t.Context(), filelock.New(path))
	defer current()

	stale()

	assertHeld(t, filelock.New(path))
}

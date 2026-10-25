// Package filelock takes an exclusive lock on a file, which the operating system releases when the
// holder closes the file or exits.
//
// New returns a Locker for one path. Every Lock opens its own descriptor on that path, so two
// Lockers in the same process exclude each other as two processes do. The lock file is created at
// 0600 when it is missing and is never removed: removing it on unlock would let a waiter that
// already opened the old file lock an inode no longer reachable by path. The directory must exist;
// the package creates none.
//
// NewNoop returns a Locker that grants every Lock at once, for callers that need no exclusion.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// pollInterval is the wait between two attempts to take a lock another descriptor holds.
const pollInterval = 15 * time.Millisecond

// ErrUnsupported is wrapped by every Lock error on a platform the package has no lock for.
var ErrUnsupported = errors.New("file locking is not supported on this platform")

// Locker takes an exclusive lock.
type Locker interface {
	// Lock blocks until it holds the lock or ctx is done, and returns the function that releases
	// the lock. Calling unlock more than once is safe; every call after the first does nothing.
	Lock(ctx context.Context) (unlock func(), err error)
}

// lockCalls is the platform's part of a lock: the system calls on an open descriptor. Each
// platform file defines two package functions with these signatures, and newLocker wires them in:
//
//	func tryLock(f *os.File) (acquired bool, err error)
//	func unlockFile(f *os.File) error
//
// tryLock makes one attempt at an exclusive lock on f and never blocks. It returns true and a nil
// error when f now holds the lock, false and a nil error when another descriptor holds it, and a
// non-nil error for any other failure, which ends Lock. The error is returned unwrapped: Lock adds
// the "filelock: lock <path>:" context.
//
// unlockFile releases the lock f holds. Its error is discarded, because f is closed next and
// closing the descriptor releases the lock in any case.
//
// Opening, retrying, honouring the context and closing are the shared loop's, not the platform's.
type lockCalls struct {
	tryLock    func(f *os.File) (acquired bool, err error)
	unlockFile func(f *os.File) error
}

// option configures a fileLocker in newLocker.
type option func(*fileLocker)

// withLockCalls replaces the platform's system calls with tryLock and unlockFile.
func withLockCalls(tryLock func(f *os.File) (bool, error), unlockFile func(f *os.File) error) option {
	return func(l *fileLocker) {
		l.calls = lockCalls{tryLock: tryLock, unlockFile: unlockFile}
	}
}

// fileLocker locks the file at path through calls.
type fileLocker struct {
	path  string
	calls lockCalls
}

// New returns a Locker on the file at path. New does not touch the file; Lock creates it when it
// is missing.
//
// Lock fails, without opening the file, when ctx is already done, even when the lock is free. A
// directory that does not exist is an error. Every error has the form
// "filelock: <op> <path>: <cause>" and wraps its cause: the context's error when ctx is done while
// waiting, ErrUnsupported on a platform without a lock.
func New(path string) Locker {
	return newLocker(path)
}

func newLocker(path string, opts ...option) *fileLocker {
	l := &fileLocker{
		path:  path,
		calls: lockCalls{tryLock: tryLock, unlockFile: unlockFile},
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Lock opens the file and attempts the lock every pollInterval until it succeeds, an attempt
// fails, or ctx is done. The context is checked before every attempt, so a done ctx always wins
// over a free lock.
func (l *fileLocker) Lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, l.wrap("lock", err)
	}

	f, err := os.OpenFile(l.path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, l.wrap("open", err)
	}

	timer := time.NewTimer(pollInterval)
	timer.Stop()

	defer timer.Stop()

	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()

			return nil, l.wrap("lock", err)
		}

		acquired, err := l.calls.tryLock(f)
		if err != nil {
			_ = f.Close()

			return nil, l.wrap("lock", err)
		}

		if acquired {
			return l.release(f), nil
		}

		timer.Reset(pollInterval)

		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
}

// release returns the unlock function for f: it releases the lock and closes f once, however many
// times it is called.
func (l *fileLocker) release(f *os.File) func() {
	var once sync.Once

	return func() {
		once.Do(func() {
			_ = l.calls.unlockFile(f)
			_ = f.Close()
		})
	}
}

func (l *fileLocker) wrap(op string, err error) error {
	return fmt.Errorf("filelock: %s %s: %w", op, l.path, err)
}

// noopLocker grants every Lock at once.
type noopLocker struct{}

// NewNoop returns a Locker whose Lock takes no lock and touches no file. It succeeds immediately
// every time, repeatedly and concurrently, and its unlock does nothing. It honours ctx as New's
// Locker does: when ctx is already done, Lock fails with "filelock: lock: <cause>" wrapping the
// context's error.
func NewNoop() Locker {
	return noopLocker{}
}

func (noopLocker) Lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("filelock: lock: %w", err)
	}

	return func() {}, nil
}

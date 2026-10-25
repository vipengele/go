// The tag is windows, where the lock is LockFileEx. lock_unix.go takes the unix systems it lists,
// and lock_other.go every other system.
//
// A handle open on the lock file keeps other processes from deleting it, which costs nothing: the
// lock file is never deleted. This file never opens or closes a handle; Lock closes the file on
// every failure path, including every error tryLock returns.
//
// CI vets and cross-compiles this file for Windows but never runs it, so its behaviour is untested
// there.

//go:build windows

package filelock

import (
	"errors"
	"math"
	"os"

	"golang.org/x/sys/windows"
)

// Every lock covers the same fixed range, the largest LockFileEx accepts, starting at offset 0, so
// two lockers exclude each other whatever offset either file is at.
const (
	rangeLow  = math.MaxUint32
	rangeHigh = math.MaxUint32
)

// tryLock makes one non-blocking attempt at an exclusive lock on f. ERROR_LOCK_VIOLATION means
// another handle holds the lock.
func tryLock(f *os.File) (bool, error) {
	err := withHandle(f, func(h windows.Handle) error {
		var overlapped windows.Overlapped

		return windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, rangeLow, rangeHigh, &overlapped)
	})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// unlockFile releases the lock f holds, over the range tryLock locked.
func unlockFile(f *os.File) error {
	return withHandle(f, func(h windows.Handle) error {
		var overlapped windows.Overlapped

		return windows.UnlockFileEx(h, 0, rangeLow, rangeHigh, &overlapped)
	})
}

// withHandle calls call with f's handle and returns its error. The handle is borrowed through
// SyscallConn, so f keeps ownership of it.
func withHandle(f *os.File, call func(h windows.Handle) error) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}

	var callErr error

	err = conn.Control(func(fd uintptr) {
		callErr = call(windows.Handle(fd))
	})
	if err != nil {
		return err
	}

	return callErr
}

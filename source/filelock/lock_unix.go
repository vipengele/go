// The tag lists the supported systems on which golang.org/x/sys provides Flock. Every other system
// builds lock_other.go or lock_windows.go.

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock makes one non-blocking attempt at an exclusive flock on f. EWOULDBLOCK, which is EAGAIN
// on every system the tag lists, means another descriptor holds the lock.
func tryLock(f *os.File) (bool, error) {
	err := flock(f, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// unlockFile releases the flock f holds.
func unlockFile(f *os.File) error {
	return flock(f, unix.LOCK_UN)
}

// flock calls unix.Flock on f's descriptor with how, retrying while a signal interrupts it. The
// descriptor is borrowed through SyscallConn, so f stays in the mode the runtime opened it in.
func flock(f *os.File, how int) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}

	var flockErr error

	err = conn.Control(func(fd uintptr) {
		for {
			flockErr = unix.Flock(int(fd), how)
			if !errors.Is(flockErr, unix.EINTR) {
				return
			}
		}
	})
	if err != nil {
		return err
	}

	return flockErr
}

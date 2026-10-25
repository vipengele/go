//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || windows)

package filelock

import "os"

// tryLock reports ErrUnsupported: this platform has no lock the package can take.
func tryLock(*os.File) (bool, error) {
	return false, ErrUnsupported
}

// unlockFile does nothing: tryLock never grants a lock here.
func unlockFile(*os.File) error {
	return nil
}

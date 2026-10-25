// Package filelock takes an exclusive lock on a file, which the operating system releases when the
// holder closes the file or exits.
//
// A Locker takes the lock: Lock blocks until it holds the lock or its context is done, and returns
// the function that releases it. New returns a Locker for one path. NewNoop returns a Locker that
// grants every Lock at once and touches no file, for callers that need no exclusion.
//
// Every Lock opens its own descriptor on the path, so two Lockers in the same process, or two Locks
// of one Locker, exclude each other as two processes do.
//
// # Context
//
// Lock checks its context on entry and before every attempt. A context that is already done fails
// Lock even when the lock is free, and the error wraps the context's error. While another
// descriptor holds the lock, Lock tries again every 15 ms until it succeeds or the context is
// done.
//
// # The lock file
//
// The lock file is created at 0600 when it is missing and is never removed: removing it on unlock
// would let a waiter that already opened the old file lock an inode no longer reachable by path.
// The directory must exist; the package creates none.
//
// A lock file left behind by a process that has exited never blocks a new process. The lock belongs
// to the open descriptor, not to the file's existence, and the operating system releases it when
// its holder exits, however it exits.
//
// # Platforms
//
// The lock is flock on linux, darwin, freebsd, netbsd, openbsd and dragonfly, and LockFileEx on
// windows. On every other system Lock fails with an error that wraps ErrUnsupported.
//
// CI cross-compiles and vets the Windows implementation but never runs it, so its behaviour is
// untested there. The behaviour tests that every platform shares are what to run on a Windows
// machine when one is available.
package filelock

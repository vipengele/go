---
about: "The file lock is the separate project source/filelock (not cli); TryLock fails fast with ErrLocked, and a pid cannot be read from the locked file on Windows, so daemon keeps a separate holder record"
saw:
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - docs/adr/0023-a-daemons-single-instance-is-a-filelock-try-lock-with-a-holder-record.md
  - source/filelock/filelock.go
  - source/filelock/lock_windows.go
  - source/filelock/lock_unix.go
  - source/filelock/doc.go
  - source/daemon/instance.go
  - CONTEXT.md
---

- ADR-0003's table says `cli` owns "a state store with a file lock"; ADR-0019 puts the lock in
  `source/filelock` (module go.vipengele.org/filelock), and ADR-0003's status line records the
  amendment. Nothing in `cli` is a lock.
- filelock.go: `Locker.Lock(ctx)` polls the unexported `lockCalls.tryLock` every 15ms
  (`pollInterval`) until acquired, error, or ctx done. `Locker.TryLock()` makes one `tryLock`
  attempt and returns an error wrapping `ErrLocked` when the lock is held (`tryLock` returns
  `(false, nil)` for that: EWOULDBLOCK on unix, ERROR_LOCK_VIOLATION on windows). A short ctx on
  `Lock` cannot tell "held" from "slow"; `TryLock` is the fail-fast path (ADR-0023).
- Unix is flock (lock_unix.go), owned by the open file description, not the process. doc.go: every
  Lock opens its own descriptor, so lockers in one process exclude each other, and closing some
  other fd on the file does not release it (unlike fcntl).
- Windows (lock_windows.go): LockFileEx exclusive over the whole range [0, 2^64-1). LockFileEx is
  mandatory, so another process reading the locked file gets ERROR_LOCK_VIOLATION (Win32
  semantics; CI only cross-compiles with `GOOS=windows go vet`). So a pid cannot live in the lock
  file: daemon writes it to a separate `instance.holder` beside `instance.lock`
  (source/daemon/instance.go), and the lock alone decides who runs.
- The lock file is never removed (doc.go): deleting on unlock would let a waiter lock an unlinked
  inode. A crash leaves the file and never blocks.
- CONTEXT.md's Lock / Lock file entries say "Avoid: pidfile"; the glossary treats the lock file's
  contents and existence as meaningless, and the Holder record is a separate term.

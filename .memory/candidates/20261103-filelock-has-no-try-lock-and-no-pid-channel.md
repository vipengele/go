---
about: "The file lock is the separate project source/filelock (not cli); it has no public try-lock or held-by-other error, and a pid cannot be read from the locked file on Windows"
saw:
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - source/filelock/filelock.go
  - source/filelock/lock_windows.go
  - source/filelock/lock_unix.go
  - source/filelock/doc.go
  - CONTEXT.md
  - source/cli/spec/paths/paths.go
---

Found while planning issue #70 (daemon single-instance guard).

- ADR-0003's table still says `cli` owns "a state store with a file lock"; ADR-0019 moved the lock to `source/filelock` (module go.vipengele.org/filelock). ADR-0003's status line records the amendment. Nothing in `cli` is a lock.
- filelock.go: `Locker.Lock(ctx)` polls tryLock every 15ms (pollInterval) until acquired, error, or ctx done. `tryLock` (unexported, in lockCalls) returns (false,nil) for "held elsewhere" (EWOULDBLOCK on unix, ERROR_LOCK_VIOLATION on windows) but that never reaches the caller: no public TryLock, no ErrHeld. Fail-fast today means a short-timeout ctx, whose error wraps context.DeadlineExceeded and cannot be told from other timeouts without more work.
- Unix is flock (lock_unix.go), owned by the open file description, not the process. doc.go: every Lock opens its own descriptor, so lockers in one process exclude each other, and closing some other fd on the file does not release it (unlike fcntl).
- Windows (lock_windows.go): LockFileEx exclusive over the whole range [0, 2^64-1). LockFileEx is mandatory, so another process reading the locked file is expected to get ERROR_LOCK_VIOLATION. This is from Win32 semantics, not tested: CI only cross-compiles (`GOOS=windows go vet`). Consequence: the pid cannot be stored in the lock file and read by a loser on Windows. Needs a separate pid file, or a byte range beyond the locked one (changes filelock's fixed range, which exists so lockers exclude whatever the file offset).
- Lock file is never removed (doc.go): deleting on unlock would let a waiter lock an unlinked inode. So a crash leaves the file and never blocks. A pid in it would be stale information whenever the lock is free.
- CONTEXT.md Lock / Lock file entries say "Avoid: pidfile". Writing a pid is not forbidden by an ADR, but the glossary treats file contents/existence as meaningless.
- cli/spec/paths Kind has only Config, State, Cache, Data, Log (paths.go:18-26). No runtime/run kind, no XDG_RUNTIME_DIR use anywhere (grep). `tumika` appears nowhere in the repo.
- Release state: no git tags exist; only docs/release-notes/commons/v0.1.0.md. So commons is in strict mode; daemon, filelock, cli/* have no notes and are in development mode. daemon/go.mod has no requires yet.

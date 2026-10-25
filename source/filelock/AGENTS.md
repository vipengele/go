# filelock

`go.vipengele.org/filelock`: an exclusive lock on a file that the operating system releases when its
holder exits, for the single-instance guard of a daemon and for serialising a check and a
replacement. Read `doc.go` first, then `docs/adr/0019-the-file-lock-is-its-own-project-filelock.md`
at the repo root. It requires no sibling project; its only dependency is `golang.org/x/sys`.

## Commands (run from `source/filelock`)

```bash
go build ./...
go vet ./...
GOOS=windows go vet ./...
GOOS=plan9 go vet ./...
go test -race -shuffle=on ./...
gofmt -l .
golangci-lint run
go mod tidy
```

## Structure

- `New(path)`, `NewNoop()`, `Locker`, `ErrUnsupported` (`filelock.go`) — `Lock` opens the file at
  0600, then tries every `pollInterval` until it holds the lock, an attempt fails or the context is
  done. The context is checked before every attempt, so a done context fails even a free lock. The
  lock file is never removed and the directory is never created.
- `lockCalls` (`filelock.go`) — the platform's part: `tryLock` (one non-blocking attempt) and
  `unlockFile`. Tests replace them with `withLockCalls`, reached through `export_test.go`
  (`WithLockCalls`, `NewWith`, `PollInterval`). The seam is a struct field and not a package
  variable, because `gochecknoglobals` is on.
- `lock_unix.go` — `flock` on linux, darwin, freebsd, netbsd, openbsd and dragonfly.
  `lock_windows.go` — `LockFileEx`. `lock_other.go` — `tryLock` returns `ErrUnsupported`.

## Build tags

`lock_unix.go`'s tag and `lock_other.go`'s negation must stay exact mirrors, with `windows` added
inside the negation because `lock_windows.go` covers it. A system in neither list, or in both,
breaks the build or links two `tryLock`s. Change the lists in both files together, and in
`lock_process_test.go` and `lock_unix_test.go`, which carry the unix tag.

## Tests

- `filelock_test.go` — the shared loop against fake lock calls, on every platform.
- `lock_unix_test.go` — real `flock` between lockers in one process.
- `lock_process_test.go` — cross-process: `TestMain` re-executes the test binary as a lock holder
  when the `FILELOCK_TEST_CHILD` sentinel is set and runs no tests in that mode. It checks that a
  holder excludes the parent until it exits and that a killed holder releases the lock.
- `example_test.go` — runnable examples; their `// Output:` check needs a supported platform.

## Gotchas

- `depguard` in `.golangci.yml` allows the standard library, `golang.org/x/sys` and this module,
  nothing else. Every dependent inherits what is added here.
- CI only cross-compiles and vets the Windows implementation. Its behaviour is untested: nothing
  runs `lock_windows.go`, and the shared behaviour tests are what to run on a Windows machine.

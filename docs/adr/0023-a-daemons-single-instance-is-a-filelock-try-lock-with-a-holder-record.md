# A daemon's single instance is a filelock try-lock with a holder record

Status: accepted. It extends ADR-0019, whose `filelock` the daemon consumes, and ADR-0009,
whose resolver gains a runtime directory kind, and it follows the shutdown order of ADR-0014.

Two instances of one daemon must not run at once on a machine. `go.vipengele.org/daemon` guards
that with an **Instance lock**: a file lock in the tool's **Runtime directory** that the operating
system releases when its holder exits. A second instance fails at once with an error that names the
holder when it can.

## The rules

- **The lock is `filelock`'s `TryLock`.** `Locker.TryLock()` makes one attempt, takes no context and
  does not poll. It returns the sentinel `ErrLocked` when another holder has the file. `Lock` and
  `TryLock` are both on `Locker`; no type outside `filelock` implements
  `Locker`, and `filelock` is unreleased, so a method on it breaks no implementation.
- **The holder record is separate and owned by `daemon`.** `instance.holder` holds the holder's
  pid in decimal and a newline, in the runtime directory beside `instance.lock`. It is written to
  the fixed name `instance.holder.tmp` and renamed into place, best effort. The lock alone decides
  who runs; a missing or failed record never fails the acquire and only enriches the error.
- **The error.** `AcquireInstance(spec.Identity, paths.Options)` returns an `*AlreadyRunningError`
  with `Path` and `PID` that matches `ErrAlreadyRunning`. `PID` is 0 when the record cannot be read.
  `Instance.Release` and `Instance.Path` complete the type.
- **The directory is a `Runtime` kind of `cli/spec/paths`.** It is `<State>/runtime` on the XDG
  layout, `<config>/runtime` on macOS, `%LocalAppData%/<app>/runtime` on Windows and `root/runtime`
  under a home root; `Options.RuntimeDir` overrides it. It is never `XDG_RUNTIME_DIR`.
- **`daemon` resolves the directory from a `spec.Identity`.** It takes the identity, not a file
  path, so it requires `cli/spec` and `filelock`. Its `depguard` rule allows `cli/spec` and still
  denies cobra and `cli/core/**`. It carries its own indirect `require` and directory `replace` of
  `commons`, because the `replace` in `cli/spec`'s `go.mod` is ignored when `daemon` builds
  (ADR-0006).
- **Supervisor integration.** `WithInstanceLock(id, opts)` makes `Run` take the lock before the
  first `Start` and release it, deferred, when `Run` returns: after the whole stop phase and never
  between two `Stop`s, in the order of ADR-0014. A held lock starts no Runner and `Run` returns the
  `*AlreadyRunningError`. `Run` marks the Supervisor as run before it takes the lock, so the
  Supervisor is not reusable after a held lock; a program that retries builds a new one.

## Accepted failure modes

- **A stale pid.** A loser can read the previous holder's pid between a winner's acquire and its
  rewrite of the record.
- **Windows sharing.** Go opens files on Windows without `FILE_SHARE_DELETE`, so a winner's rename
  or remove can fail while a loser reads the record, which leaves the previous pid in place.
- **A stray temporary file.** The temporary name is fixed, so a crash leaves at most one
  `instance.holder.tmp`, which the next write overwrites.
- **PID 0.** An unreadable or malformed record reports pid 0.
- **An abandoned Runner.** A Runner abandoned at the end of the grace period may still be running
  when the lock is released. Run returning is the only release point that keeps shutdown bounded,
  so a consumer whose Runners ignore `Stop` can see a second instance start beside it.
- **Windows is covered by injection.** CI vets Windows only for a project that holds a
  `*_windows.go` file (ADR-0019). `daemon` has none and gets no artificial one: its Windows
  behaviour is covered by tests that inject file-system failures, and by a manual
  `GOOS=windows go vet ./...`.

## Considered options

- **`Lock` with a short context.** A deadline poll cannot tell a held lock from a slow one, so a
  busy machine reports a running instance that is not there. `TryLock` makes one attempt and the
  answer is exact.
- **The pid inside the lock file.** `LockFileEx` on Windows is mandatory, so a loser cannot read a
  byte of the locked file. Putting the record in `filelock` would also give the lock file content
  that means something; kept in `daemon`, the lock file's content is meaningless (ADR-0019).
- **`XDG_RUNTIME_DIR`.** The system removes it when the user's last session ends. A daemon that
  outlives the session would hold a lock on an unlinked inode while the next start locks a fresh
  file, and two instances would run.
- **A temporary directory.** Cleaners remove files from it for the same effect.
- **A file path from the caller.** It would leave every daemon to choose a directory and to find one
  that survives logout. The identity gives one resolver and one answer.
- **Releasing between Stops.** A second instance could start while a Runner of the first still
  holds its port or its data.
- **Releasing at the start of shutdown.** The same, for every Runner.
- **Deciding by the pid in the record.** A pid is reused and a record outlives a crash; only the
  operating system knows whether the holder is alive.

## Out of scope

`sd_notify`, reload, health and the mapping of the error to an exit code.

## Consequences

- **`daemon` requires `cli/spec` and `filelock`.** A change to either selects `daemon` in CI
  (ADR-0006).
- **The runtime directory is a new kind** in `paths`, resolved only for a caller that asks for it.
- **`filelock` is released before `daemon`** (ADR-0006).

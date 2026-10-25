# The file lock is its own project, `filelock`

Status: accepted. It refines the "state store with a file lock" row of the project table in
`0003-the-library-is-three-projects-cli-update-and-daemon.md`, whose lock leaves `cli`, and
`0018-the-state-store-is-its-own-project-cli-state.md`, whose store takes no lock. ADR-0009 is not
amended: it binds the platform-paths resolver only.

Two processes that write the same state file, or two instances of the same daemon, need a lock the
operating system releases when its holder exits. `daemon` needs it for the single-instance guard and
`update` needs it to serialise its check and its replacement of the managed binary, so the lock must
be reachable by `update`, which may not require `cli/core` (ADR-0005), as well as by `daemon`. `go.vipengele.org/filelock` provides it, with the module path of
`source/filelock` (ADR-0004).

## Decisions

- **Home.** `source/filelock`, a top-level project with its own `go.mod`, lydite component, `gomod`
  entry and `go.work` line. `daemon` and `update` both require it.
- **OS calls.** The lock is taken with `golang.org/x/sys`: `unix.Flock` on Unix systems and
  `windows.LockFileEx` and `windows.UnlockFileEx` on Windows, in build-tagged per-OS files.
  `golang.org/x/sys` is a direct requirement of this project only. The project's `.golangci.yml`
  carries a `depguard` rule that allows the standard library and `golang.org/x/sys` and nothing
  else.
- **Windows verification.** CI runs `GOOS=windows go vet ./...` for any affected project that has
  `*_windows.go` files, after `golangci-lint`, and writes no artefacts. It is a cross-compile, not a
  Windows runner: the Windows implementation is real (`LockFileEx`) but its behaviour is untested in
  CI.

## Considered options

- **`cli/state`.** ADR-0018 gives it paths and nothing more, and no lock. Putting the lock there
  would also push `golang.org/x/sys` onto every importer of the store, including those that never
  lock.
- **`cli/spec/paths`.** ADR-0009 draws `cli/spec`'s I/O boundary around resolving and creating
  directories. A lock is neither, and a syscall dependency would reach every project that requires
  `cli/spec`.
- **`commons`.** It is the project of generic building blocks and requires no sibling (ADR-0004);
  its `go.mod` has no dependencies today. Adding `golang.org/x/sys` there would push a syscall
  dependency onto every project that requires it.
- **`cli/core`, as an internal package.** An `internal` package is importable by `cli/core` alone,
  and `update` may not require `cli/core` (ADR-0005), so neither `update` nor `daemon` could reach
  it and each would keep a copy.
- **A run-time `Options.GOOS` seam, as in ADR-0009.** ADR-0009 selects a path layout at run time so
  one host tests all three layouts. A lock is a syscall: `flock` and `LockFileEx` differ in
  behaviour, not in a table of strings, and no run-time seam can make one host call the other's
  system call. Build-tagged per-OS files are the right shape here. This is a different problem from
  the one ADR-0009 decides, not a reversal of it.
- **A Windows CI runner.** The cost ADR-0009 rejected, for every project. A cross-compile with
  `go vet` catches a Windows file that does not type-check, which is the failure a Linux-only CI
  would otherwise miss.
- **A lock file created by `O_EXCL`.** A crashed holder leaves the file behind and the next process
  cannot tell a live holder from a dead one. An OS lock is released when the process exits.

## Consequences

- **The lock has no shared dependency.** `golang.org/x/sys` enters a consumer's graph through
  `filelock` alone; `cli/state`, `cli/spec` and `commons` keep their dependency sets.
- **The Windows path is checked for compilation only.** A behavioural fault in the `LockFileEx`
  implementation reaches a release unseen by CI.
- **CI learns a per-OS rule.** The `GOOS=windows go vet` step applies to any project with
  `*_windows.go` files, not to `filelock` by name.
- **A new project to release.** `filelock` is tagged `source/filelock/vX.Y.Z` and resolves through
  `go.vipengele.org` once `publish-vanity-modules.yml` has run (ADR-0001, ADR-0016). A change to it
  selects every project that requires it in CI (ADR-0006).

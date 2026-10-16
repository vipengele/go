# daemon

`go.vipengele.org/daemon`: runs the long-running work of a process as Runners under a Supervisor.
Read `doc.go` first, then `docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md`
at the repo root.

## Commands (run from `source/daemon`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Structure

- `runner.go` — the `Runner` interface: `Name`, a blocking `Start`, and `Stop`.
- `supervisor.go` — `New` validates the Runners (non-empty, no nil, unique non-blank names) and
  `Run` starts them, then stops them in reverse order within one grace period. `guard` recovers
  panics into `*PanicError`; `launched` tracks one started Runner.
- `option.go` — `WithGrace` and `WithPanicPolicy` (`PanicFail`, the default, or `PanicIsolate`).
- `errors.go` — `PanicError` and the sentinel errors (`ErrRunnerExited`, `ErrStopTimeout`,
  `ErrAlreadyRan`).

## Conventions

- The package requires nothing beyond the standard library.
- The grace period's context is made from `context.Background()`, never from the cancelled run context.
- No signal handling: the context passed to `Run` drives shutdown; `cli/core` owns the signals.
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- `depguard` forbids importing `github.com/spf13/cobra` and `go.vipengele.org/cli/core/**`.
- A new `Run` outcome is documented in the `Errors` section of `doc.go` and tested in
  `supervisor_test.go`.

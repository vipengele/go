# daemon

`go.vipengele.org/daemon`: runs the long-running work of a process as Runners under a Supervisor,
and guards it with an instance lock so one instance of a tool runs at a time.
Read `doc.go` first, then, at the repo root,
`docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md` (the Runner
contract) and `docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md`
(reverse stop order within one grace period).

## Commands (run from `source/daemon`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Structure

- `runner.go` — the `Runner` interface: `Name`, `Start` that returns once the Runner is ready,
  `Done` and `Err` that report its exit, and `Stop`; the optional `Order() int` method.
- `supervisor.go` — `New` validates the Runners (non-empty, no nil, unique non-blank names) and
  sorts them stably by Order. `Run` starts them in turn within the start timeout, waits until the
  run context is done or a Runner exits, then calls `Stop` once on every Runner whose `Start` was
  called, in reverse start order within one grace period, and waits for its `Done`. `guard`
  recovers panics into `*PanicError`; `launched` tracks one Runner whose `Start` was called. With
  `WithInstanceLock`, `Run` takes the instance lock before the first `Start` and releases it in a
  `defer`, after the stop phase, on every return path.
- `instance.go` — `AcquireInstance`, the instance lock (`instance.lock`, through
  `go.vipengele.org/filelock`) in the tool's runtime directory from `cli/spec/paths`, and the
  best-effort holder record (`instance.holder`); `Instance` with `Path` and an idempotent
  `Release`.
- `func.go` — `Func`, the Runner adapter for a function run in its own goroutine, and `WithOrder`
  (`OrderOption`, shared with the HTTP Runner).
- `http.go` — `NewHTTPServer` and `NewHTTPServerListener`, the `*http.Server` Runner, with
  `WithHTTPName` and `DefaultHTTPOrder`.
- `restart.go` — `RestartRequested`, true only when every leaf of an error is `ErrRestartRequired`
  and none is a `*PanicError`.
- `option.go` — `WithGrace`, `WithStartTimeout`, `WithPanicPolicy` (`PanicFail`, the default, or
  `PanicIsolate`) and `WithInstanceLock`, which `New` rejects for an identity that fails
  `Validate`.
- `errors.go` — `PanicError` and the sentinel errors (`ErrRunnerExited`, `ErrStartTimeout`,
  `ErrStopTimeout`, `ErrAlreadyRan`, `ErrRestartRequired`, `ErrAlreadyRunning`) and
  `AlreadyRunningError`, which matches `ErrAlreadyRunning`; `ErrNilFunc` is in `func.go`,
  `ErrNilServer` and `ErrNilListener` in `http.go`.

## Conventions

- The package is not stdlib-only: it requires `go.vipengele.org/filelock` and
  `go.vipengele.org/cli/spec` (for `spec.Identity` and `cli/spec/paths`), which bring in
  `go.vipengele.org/commons` and `golang.org/x/sys` indirectly.
- The grace period's context is made from `context.Background()`, never from the cancelled run context.
- Work that outlives `Start` is never bound to the start context, which is cancelled when the start
  phase ends: `Func` and the HTTP Runner derive theirs with `context.WithoutCancel`.
- No signal handling: the context passed to `Run` drives shutdown; `cli/core` owns the signals.
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- `depguard` forbids importing `github.com/spf13/cobra` and `go.vipengele.org/cli/core/**`;
  `cli/spec` is allowed.
- The instance lock is released only after every Runner has stopped or been abandoned at the end
  of the grace period, never between two Stops.
- A new `Run` outcome is documented in the `Errors` section of `doc.go` and tested in
  `supervisor_test.go`, or, for the instance lock, `supervisor_instance_test.go`.

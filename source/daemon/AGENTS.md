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
  `Done` and `Err` that report its exit, and `Stop`; the optional `Order() int` method; the
  optional `HealthChecker` interface, whose `CheckHealth` becomes a Readiness check named after
  the Runner.
- `supervisor.go` — `New` validates the Runners (non-empty, no nil, unique non-blank names) and
  sorts them stably by Order. `Run` starts them in turn within the start timeout, waits until the
  run context is done or a Runner exits, then calls `Stop` once on every Runner whose `Start` was
  called, in reverse start order within one grace period, and waits for its `Done`. `guard`
  recovers panics into `*PanicError`; `launched` tracks one Runner whose `Start` was called and
  is touched only by Run's goroutine. The health feed is separate, mutex-guarded state Run
  publishes at each transition (`feed`, `runPhase`, `runnerPhase`); `New` registers
  `supervisor.live`, `supervisor.ready` and each `HealthChecker` in the `WithHealth` registry as
  its last step, all or none. With `WithInstanceLock`, `Run` takes the instance lock before the
  first `Start` and releases it in a `defer`, after the stop phase, on every return path.
- `instance.go` — `AcquireInstance`, the instance lock (`instance.lock`, through
  `go.vipengele.org/filelock`) in the tool's runtime directory from `cli/spec/paths`, and the
  best-effort holder record (`instance.holder`); `Instance` with `Path` and an idempotent
  `Release`.
- `health.go` — `Health`, built with `NewHealth` and `WithCheckTimeout`: `Add` registers a `Check`
  of `Kind` `Liveness` and/or `Readiness`; `Snapshot` runs every check concurrently under its
  timeout and returns a `Snapshot` (`ok`, `unready` or `down`, plus `Live`, `Ready`, uptime and a
  `CheckResult` per check) whose JSON keys are the wire contract of
  `docs/adr/0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md`.
- `health_http.go` — `(*Health).Handler`, serving `/livez`, `/readyz` and `/status`;
  the `/status` body is written by `spec.WriteJSON` at schema 1.
- `func.go` — `Func`, the Runner adapter for a function run in its own goroutine, and `WithOrder`
  (`OrderOption`, shared with the HTTP Runner).
- `http.go` — `NewHTTPServer` and `NewHTTPServerListener`, the `*http.Server` Runner, with
  `WithHTTPName` and `DefaultHTTPOrder`; it serves TLS with `ServeTLS` when the server's
  `TLSConfig` is non-nil, and `Start` fails with `ErrNoCertificate` when that config carries no
  certificate (`Certificates`, `GetCertificate` or `GetConfigForClient`).
- `restart.go` — `RestartRequested`, true only when every leaf of an error is `ErrRestartRequired`
  and none is a `*PanicError`.
- `option.go` — `WithGrace`, `WithStartTimeout`, `WithHealth`, `WithPanicPolicy` (`PanicFail`,
  the default, or `PanicIsolate`) and `WithInstanceLock`, which `New` rejects for an identity that
  fails `Validate`.
- `errors.go` — `PanicError` and the sentinel errors (`ErrRunnerExited`, `ErrStartTimeout`,
  `ErrStopTimeout`, `ErrAlreadyRan`, `ErrRestartRequired`, `ErrAlreadyRunning`),
  `AlreadyRunningError`, which matches `ErrAlreadyRunning`, and the health sentinels
  (`ErrBlankCheckName`, `ErrDuplicateCheck`, `ErrNilCheck`, `ErrUnknownKind`, `ErrCheckTimeout`);
  `ErrNilFunc` is in `func.go`, `ErrNilServer`, `ErrNilListener` and `ErrNoCertificate` in `http.go`.
- `daemoncmd/` — `NewStatus`, the `status` command: an HTTP client of `/status` that prints the
  snapshot as tables through `cli/spec/table`, or as the same JSON through `spec.WriteJSON` when
  the App's output is JSON, and returns `spec.Reported(ErrNotOK)` when the daemon is not ok.
  `testdata/status.keys` pins its JSON keys.

- `httpserver/` — package `go.vipengele.org/daemon/httpserver`: `Error`, the JSON error envelope, and
  `DecodeJSON`, which returns a `*RequestError`.

## The `service` package

`go.vipengele.org/daemon/service` fronts the operating system's service manager (systemd, launchd,
the Windows service control manager) behind the `Manager` interface: `Prepare`, `Install`,
`Uninstall`, `Start`, `Stop` and `Status`, each taking only a context. Read its `doc.go` first and
`docs/adr/0024-service-drivers-are-selected-at-runtime.md`.

- `manager.go` — `Manager` and `New(cfg, opts...)`, which validates the `Config` against the target
  GOOS and then switches on it to select the driver.
- `option.go` — `Option` and `WithGOOS`; `""` means `runtime.GOOS`, `"windows"` applies the Windows
  rules and every other value the POSIX rules.
- `config.go` — `Config` (`Name`, `Description`, `Binary`, `Args`, `Env`, `WorkingDir`, `User`,
  `Scope`) and `Validate`.
- `state.go` — `State` (unknown, not installed, stopped, starting, running, failed) and `Status`.
- `errors.go` — the sentinel errors, `ErrInvalidConfig` and `ErrUnsupported` among them.

Conventions of the package:

- Validation follows the target GOOS, never the host's: no `filepath.IsAbs`. The rules come
  from the target GOOS; `runtime.GOOS` is only the default target (`New` without `WithGOOS`, and
  `Config.Validate`). Tests render and validate any OS's service on any host through `WithGOOS`.
- No build tags and no `_windows.go` file. Only the operating-system calls themselves, the Windows
  service control manager, could ever need one.
- `State` names go through `switch` in `String`, `MarshalText` and `UnmarshalText`, never a
  package-level table: `gochecknoglobals` is enabled.
- `Validate` returns `errors.Join` of `ErrInvalidConfig`-wrapped violations in a fixed order: fields
  in declaration order, `Args` by index, `Env` by sorted key.
- Standard library only.

## Conventions

- The root `daemon` package is not stdlib-only: it requires `go.vipengele.org/filelock` and
  `go.vipengele.org/cli/spec` (for `spec.Identity` and `cli/spec/paths`), which bring in
  `go.vipengele.org/commons` and `golang.org/x/sys` indirectly. Only `daemoncmd` imports any
  other `cli/spec` package.
- The grace period's context is made from `context.Background()`, never from the cancelled run context.
- Work that outlives `Start` is never bound to the start context, which is cancelled when the start
  phase ends: `Func` and the HTTP Runner derive theirs with `context.WithoutCancel`.
- No signal handling: the context passed to `Run` drives shutdown; `cli/core` owns the signals.
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- `depguard` forbids importing `github.com/spf13/cobra` and `go.vipengele.org/cli/core` anywhere,
  and, in every file outside `daemoncmd/`, any `go.vipengele.org/` package but the module's own
  `daemon`, `daemon/service` and `daemon/httpserver`, `cli/spec`, `cli/spec/paths` and `filelock`. A depguard `pkg`
  entry matches by prefix, so a `/**` suffix matches nothing.
- The instance lock is released only after every Runner has stopped or been abandoned at the end
  of the grace period, never between two Stops.
- The `/status` body and the `status` command's JSON are byte-identical: both are written by
  `spec.WriteJSON` with the same schema, which `TestStatusJSONIsTheHandlerBody` in
  `daemoncmd/status_test.go` pins.
- A new `Run` outcome is documented in the `Errors` section of `doc.go` and tested in
  `supervisor_test.go`, or, for the instance lock, `supervisor_instance_test.go`.

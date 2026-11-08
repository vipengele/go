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
  first `Start` and releases it in a `defer`, after the stop phase, on every return path. With
  `WithSystemd` the watchdog is a mutex-guarded `{stop, stopped}`: armed after `READY=1`, disarmed
  when the stop phase begins, and it pings only while the unexported snapshot reports live.
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
- `reload.go` — `Reloader[T]`, the optional Runner interface for applying a new configuration, and
  `WithReload[T]`, which makes `Run` load and offer it on SIGHUP.
- `option.go` — `WithGrace`, `WithStartTimeout`, `WithHealth`, `WithPanicPolicy` (`PanicFail`,
  the default, or `PanicIsolate`), `WithInstanceLock`, which `New` rejects for an identity that
  fails `Validate`, `WithLogger`, `WithClock` (the Clock seam), `WithReady` (the Ready seam) and `WithSystemd`. Tests:
  `systemd_test.go`.
- `clock.go` — the `Clock` interface (`Now`, `AfterFunc`), the system clock, and `withDeadline`
  with its `clockContext`, the deadline context for any other clock. `Deadline` is the clock's
  deadline or the parent's when earlier, and `Err` is `context.DeadlineExceeded` when the clock's
  timer fires, the parent's error when the parent ends, or `context.Canceled` on cancel, whichever
  comes first. `Health` takes its own clock through `WithHealthClock`, and its check context
  carries no `context.Cause`, so `Health` never reads it from that context (only from the caller's).
  Tests: `clock_test.go`, and `ready_test.go` for `WithReady`.
- `daemontest/` — package `go.vipengele.org/daemon/daemontest`, the test kit (ADR-0028), written
  against the exported API only. `Clock` is a fake whose time moves only on `Advance`, with
  `BlockUntilTimers` to wait until the code under test has armed its timers. `NewRunner` returns a
  scripted Runner (`FailStart`, `HangStart`, `IgnoreStop`, `StopErr`, `Order`) that records its
  calls. `Start` runs a Supervisor on `127.0.0.1:0` and registers a cleanup that releases every
  hang and then stops it.
- `systemd/` — package `go.vipengele.org/daemon/systemd` (ADR-0030). `notify.go` holds `Notify`
  and the state constants, `watchdog.go` `WatchdogInterval`, `listeners.go` `Listeners` and
  `UnknownName`. The seams are parameters of the unexported functions (`goos`, and `firstFD` for
  `listeners`), never package variables; `Listeners` is called once per process. Tests re-exec the
  test binary to pass descriptors through `ExtraFiles`.
- `errors.go` — `PanicError` and the sentinel errors (`ErrRunnerExited`, `ErrStartTimeout`,
  `ErrReloadTimeout`, `ErrStopTimeout`, `ErrAlreadyRan`, `ErrRestartRequired`, `ErrAlreadyRunning`),
  `AlreadyRunningError`, which matches `ErrAlreadyRunning`, and the health sentinels
  (`ErrBlankCheckName`, `ErrDuplicateCheck`, `ErrNilCheck`, `ErrUnknownKind`, `ErrCheckTimeout`);
  `ErrNilFunc` is in `func.go`, `ErrNilServer`, `ErrNilListener` and `ErrNoCertificate` in `http.go`.
- `daemoncmd/` — `NewStatus`, the `status` command: an HTTP client of `/status` that prints the
  snapshot as tables through `cli/spec/table`, or as the same JSON through `spec.WriteJSON` when
  the App's output is JSON, and returns `spec.Reported(ErrNotOK)` when the daemon is not ok.
  `testdata/status.keys` pins its JSON keys.
- `httpserver/` — package `go.vipengele.org/daemon/httpserver`, the hardened `*http.Server` the
  HTTP Runner serves; read its `doc.go` and
  `docs/adr/0026-the-http-server-kit-has-a-fixed-middleware-order-and-pluggable-auth.md`.
  - `server.go` — `New`, which wraps the handler in the fixed middleware chain and sets the
    timeouts and limits, `CheckListener` and the plain-HTTP exposure warning.
  - `option.go` — `Option` and the `With...` options, and `Timeouts`.
  - `middleware.go` — `Recover`, `Log`, `SecureHeaders`, `AllowHosts`, `AllowOrigins` and
    `LimitBody`.
  - `auth.go` — `Authenticator`, `BearerToken` and `RequireAuth`, which skips the canonical
    unauthenticated paths.
  - `errors.go` — `Error`, the JSON error envelope written by `spec.WriteJSON`.
  - `decode.go` — `DecodeJSON` and `*RequestError`.

## The seams

- Clock: `WithClock` for the Supervisor's start timeout, grace period and reload deadline,
  `WithHealthClock` for `Health`. A test passes `daemontest.Clock` and advances it.
- Ready: `WithReady` is called once per `Run`, after every `Start` has returned, synchronously on
  `Run`'s goroutine.
- Environment: `WithSystemd` and the `systemd` package take a `lookup func(key string)
  (string, bool)`, never `os.Getenv`; a test passes a map-backed one.

## The `service` package

`go.vipengele.org/daemon/service` fronts the operating system's service manager (systemd, launchd,
the Windows service control manager) behind the `Manager` interface: `Prepare`, `Install`,
`Uninstall`, `Start`, `Stop` and `Status`, each taking only a context. Read its `doc.go` first and
`docs/adr/0024-service-drivers-are-selected-at-runtime.md`.

- `manager.go` — `Manager` and `New(cfg, opts...)`, which checks, in order, the `WithIdentity`
  identity (`ErrInvalidConfig`), whether the process runs in a container (`*ContainerError`), the
  `Config` against the target GOOS, the `WithSystemdHardening` override, the `WithSystemdNotify` validation, and then switches on the
  GOOS to select the driver (systemd on linux, `ErrUnsupported` elsewhere).
- `option.go` — `Option`, `WithGOOS`, `WithIdentity`, `WithContainer`,
  `WithSystemdHardening` and `WithSystemdNotify`; `""` means `runtime.GOOS`, `"windows"` applies the Windows rules and every other value the POSIX rules.
  `WithContainer` replaces container detection and wins over the environment and `WithIdentity`;
  without it, `New` asks `paths.InContainerFor` with the identity and `paths.InContainer` without
  one.
- `config.go` — `Config` (`Name`, `Description`, `Binary`, `Args`, `Env`, `WorkingDir`, `User`,
  `Scope`) and `Validate`.
- `state.go` — `State` (unknown, not installed, stopped, starting, running, failed) and `Status`.
- `errors.go` — the sentinel errors, `ErrInvalidConfig`, `ErrUnsupported`, `ErrNotInstalled`,
  `ErrPrivilegesRequired`, `ErrManagerUnreachable` and `ErrContainer` among them, and
  `ContainerError`, which matches `ErrContainer` and carries the `Instead` advice.
- `hardening.go` — `Hardening`, `DefaultHardening(Scope)` and its validation; set with
  `WithSystemdHardening`, which `New` validates on every GOOS and the other drivers ignore
  (`docs/adr/0028-driver-specific-settings-are-options-not-config-fields.md`).
- `systemd.go`, `systemd_unit.go`, `systemd_status.go` — the systemd driver: the unexported
  `systemd` struct, the unit rendering and `Status`.
- `export_test.go` — `WithLookupEnv`, the test-only seam for the environment container detection
  reads.

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
- Every test passes `WithContainer` (or `WithLookupEnv` with a forced `<PREFIX>_CONTAINER`, or a
  `container` variable) so that no test result depends on the host being a container.
- Standard library only, apart from `go.vipengele.org/cli/spec` and `cli/spec/paths`, for
  `spec.Identity` and container detection.

The systemd driver:

- Scopes. A system unit lives in `/etc/systemd/system`, is driven by `systemctl`, is `WantedBy=
  multi-user.target` with `After=`/`Wants=network-online.target`, and `Install`, `Uninstall`,
  `Start`, `Stop` and `Prepare` need root (`ErrPrivilegesRequired`). A user unit lives in
  `$XDG_CONFIG_HOME/systemd/user` (default `$HOME/.config/systemd/user`), is driven by
  `systemctl --user`, is `WantedBy=default.target` with no network ordering, and needs no root;
  `Config.User` must be empty with `ScopeUser` on POSIX. `Type=simple` by default, `Type=notify`
  (plus `WatchdogSec=` in whole ms) with `WithSystemdNotify`; `Restart=always` and `RestartSec=5`
  are fixed.
- `Install` is: privileges, account check (`id -u -- <user>` for a system unit with a `User`; the
  driver never creates an account, a missing one fails before anything is written, a missing `id` is
  `ErrUnsupported`), `Prepare` (creates the unit directory only), atomic write (temp file, chmod
  0644, rename), `daemon-reload`, `enable`, `restart`. Never `enable --now`: it leaves an already
  running service on its old unit and binary, while `restart` starts a stopped service and replaces
  a running one, so `Install` also applies an update.
- `Uninstall` with no unit file is `ErrNotInstalled`. A failing `disable --now` does not stop the
  removal of the unit file and the `daemon-reload`, and `Uninstall` then returns an error saying the
  disable failed; data and the account stay. `Start` and `Stop` are `ErrNotInstalled` without a unit
  file.
- Escaping: values are escaped, never rejected.

  | Directive | Rule |
  |---|---|
  | `ExecStart=` | each word double-quoted, `\` and `"` backslash-escaped, `%` to `%%`, `$` to `$$`, an empty argument is `""` |
  | `Environment=` | one line per key in sorted order, `"K=v"` quoted and escaped, `%` to `%%`, no `$$` (systemd does not expand variables there) |
  | `ReadWritePaths=` | each path a quoted word, `%` to `%%` |
  | `WorkingDirectory=` | raw, `%` to `%%`, and a `/` appended when it ends in `\`, which names the same directory |
  | `Description=` | raw, `%` to `%%`, and a trailing run of `\` dropped; no line when nothing is left |

  systemd joins a line ending in an unescaped `\` with the next one, so no rendered line ends in
  `\` (`checkNoContinuation` runs over every golden). Newlines never reach a raw value: `Validate`
  rejects control characters. `Environment=` values are readable by every local account (the unit
  is 0644 and `systemctl show` reports them), so `Config.Env` must not carry secrets.

- Hardening: system defaults are `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=full`,
  `ProtectHome=read-only`, `ProtectKernelTunables`, `ProtectControlGroups` and `RestrictSUIDSGID`; a
  user unit gets `NoNewPrivileges` only, because sandboxing directives in a user unit need
  unprivileged user namespaces. When `ProtectSystem` or `ProtectHome` is set, `WorkingDir` is
  appended to `ReadWritePaths`.
- `Status` is one `systemctl show --property=...`. `ActiveState` maps to a `State`; `Result=success`
  passes `restarts=0` to `resolve`, so a restart after a clean exit (an update, or a requested
  restart that `restart.go` turns into exit 0) reads Starting, not Failed, while a crash loop reads
  Failed with a `journalctl` hint. A missing `NRestarts` (systemd before 235) counts as 0. When
  `systemctl --user` cannot reach the user manager (`sudo -u`, cron, no session), `Status` returns
  `StateUnknown` with a `Detail` and `ErrManagerUnreachable`. Lingering is never turned on; for a
  user unit `Detail` says it is off when `/var/lib/systemd/linger/<user>` is absent. A missing
  `systemctl` is `ErrUnsupported`; `New` never probes.
- Test seam: the unexported `systemd` struct holds its operating-system touch points as fields
  (`runner func(ctx, argv) ([]byte, error)`, `euid`, `lookupEnv`, `lingerDir`, `systemDir`). `New`
  fills in the real ones and tests build the struct with fakes. There is no exported seam and no
  package-level variable (`gochecknoglobals` and `depguard` apply to tests).
- Goldens are `testdata/systemd/*.golden`; `go test ./service -update` (from `source/daemon`)
  rewrites them. The flag is registered in `TestMain` in `main_test.go`.

## Conventions

- The root `daemon` package is not stdlib-only: it requires `go.vipengele.org/filelock` and
  `go.vipengele.org/cli/spec` (for `spec.Identity` and `cli/spec/paths`), which bring in
  `go.vipengele.org/commons` and `golang.org/x/sys` indirectly. Only `daemoncmd` imports any
  other `cli/spec` package.
- The grace period's context is made from `context.Background()`, never from the cancelled run context.
- Work that outlives `Start` is never bound to the start context, which is cancelled when the start
  phase ends: `Func` and the HTTP Runner derive theirs with `context.WithoutCancel`.
- The package handles only SIGHUP, and only under `WithReload`: the context passed to `Run` drives
  shutdown, and SIGINT and SIGTERM stay with the caller (`cli/core` owns them).
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- `depguard` forbids importing `github.com/spf13/cobra` and `go.vipengele.org/cli/core` anywhere,
  and, in every file outside `daemoncmd/`, any `go.vipengele.org/` package but the module's own
  `daemon`, `daemon/service`, `daemon/httpserver` and `daemon/systemd`, `cli/spec`, `cli/spec/paths` and
  `filelock`. A depguard `pkg` entry matches by prefix, so a `/**` suffix matches nothing.
- The instance lock is released only after every Runner has stopped or been abandoned at the end
  of the grace period, never between two Stops.
- The `/status` body and the `status` command's JSON are byte-identical: both are written by
  `spec.WriteJSON` with the same schema, which `TestStatusJSONIsTheHandlerBody` in
  `daemoncmd/status_test.go` pins.
- `httpserver` imports the standard library and `go.vipengele.org/cli/spec` (for `spec.WriteJSON`)
  only, and its middleware order is fixed (ADR-0026): a new middleware gets a position in the chain
  in `New`, documented under `# Middleware order` in its `doc.go`, never an option that reorders.
- A new `Run` outcome is documented in the `Errors` section of `doc.go` and tested in
  `supervisor_test.go`, or, for the instance lock, `supervisor_instance_test.go`, and, for reload,
  `reload_test.go` (the real SIGHUP in `reload_signal_test.go`). Log records are tested in
  `logging_test.go`.
- `daemon`'s own tests cannot import `daemontest`: it imports `daemon`, an import cycle. They use
  their own fakes.
- `depguard` lets only `daemoncmd` import `daemontest`, so `daemontest`'s own tests are internal
  (`package daemontest`).
- `gochecknoglobals` covers test files too.
- Every `daemontest` hang must be released (`Runner.Release`, done by the `Start` cleanup): an
  unreleased one is an abandoned goroutine (ADR-0014).
- A `daemontest.Start` call with a `HangStart` runner does not return until the start timeout, so
  start-timeout tests drive `daemon.New(...).Run` directly.

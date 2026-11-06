---
about: "What binds a SIGHUP reload path in daemon: no signal handling in daemon, no cli/core dependency (depguard), the loader is config.Load behind cli/core/command/config Options.Load, no _windows.go convention for daemon, and no prior reload attempt or ADR"
saw:
  - source/daemon/reload.go
  - source/daemon/supervisor.go
  - source/daemon/option.go
  - docs/adr/0025-a-daemon-reloads-its-config-on-sighup-through-an-opt-in-option.md
  - source/daemon/doc.go
  - source/daemon/runner.go
  - source/daemon/.golangci.yml
  - source/daemon/go.mod
  - source/config/config.go
  - source/config/validate.go
  - source/cli/core/config.go
  - source/cli/core/command/config/config.go
  - source/cli/core/execute.go
  - docs/adr/0012-signals-are-registered-by-execute-and-injected-through-env.md
  - docs/adr/0012-configuration-is-a-provider-based-project.md
  - docs/adr/0023-a-daemons-single-instance-is-a-filelock-try-lock-with-a-holder-record.md
  - docs/adr/0024-service-drivers-are-selected-at-runtime.md
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
---

Checked 2026-11-05 while planning vipengele/go#72 (Reload on SIGHUP).

- "The cli loader" is really the `config` project (ADR-0012-configuration): `config.Load` at
  `source/config/config.go:78`; `cli/core` only wraps it (`source/cli/core/config.go:54-61`,
  `command/config/config.go:63-69` `Options[T].Load(inv)` = `cfg.Load(defaults, File, Env)`).
  Stateless, no flags layer, no caching, no process env read (caller passes LookupEnv), so it is
  safe to call repeatedly. `Options.Load` dereferences `inv.Env.LookupEnv` unless `LookupEnv` is set.
- Errors: `Load` returns the zero Result on any problem, one `errors.Join` (config.go:62-70, 117-123);
  `Validator.Validate` failures keep `*FieldError` annotated with value+origin (validate.go:19-75).
  Old config can be kept simply by not replacing it on err.
- daemon must not require `cli/core`: `source/daemon/.golangci.yml` depguard denies cobra and
  `go.vipengele.org/cli/core/**`; `source/daemon/go.mod` requires only `cli/spec` and `filelock`.
  `config` has no sibling requires (ADR-0012-configuration Consequences), so daemon -> config makes
  no cycle (cli/core -> config, daemon -/-> cli/core). Needs `require` + `replace ../config`
  (ADR-0006), and `commons` replace already present (see 20261103-daemon-carries-its-own-commons-replace).
- Signals: daemon never subscribes to SIGINT/SIGTERM; those are registered only by `cli/core`
  `notifyInterrupts` (execute.go:~70-100, ADR-0012-signals). daemon takes SIGHUP only under the
  opt-in `WithReload[T]` (`source/daemon/reload.go`, ADR-0025, `doc.go` "# Signals" at :183),
  subscribing in `Run` after the instance lock (`supervisor.go:196-199`).
- Optional-interface precedent: `orderer` (`daemon/runner.go:36-45`), `config.Validator`,
  `RemovedKeys()` (`config/schema.go:56`).
- Windows: ADR-0023 (daemon "has none and gets no artificial one", Windows covered by injection and
  manual `GOOS=windows go vet`), ADR-0024 (runtime GOOS selection, no build tags), ADR-0019
  (`*_windows.go` turns on CI `GOOS=windows go vet`; build tags only for syscalls). Only
  `source/filelock` has `lock_unix.go/lock_windows.go/lock_other.go`. `syscall.SIGHUP` does not exist
  on windows (it does in the syscall package for windows in recent Go, but is never delivered).
- The reload path is `WithReload[T]` with the consumer passing a closure over `Options.Load`
  (`doc.go` "# Reload" at :129); daemon still does not import `config` or `cli/core`.
- daemon logs through `WithLogger(*slog.Logger)` (`source/daemon/option.go`); unset, `Run` resolves
  `slog.Default()` when it begins (`supervisor.go:178`), so the `slog.SetDefault` cli/core's
  `WithLogging` makes in a pre-run hook (`cli/core/logging.go`) is the logger used.

---
about: "How a daemon reload round decides rejected vs fatal, which Runner a timeout names, and why a deadline-between-Reloads test cannot use expireReload alone"
saw:
  - source/daemon/supervisor.go
  - source/daemon/reload.go
  - source/daemon/reload_test.go
  - source/daemon/logging_test.go
---

- One round = one context from `s.newReloadContext(ctx)` shared by `load` and every `Reload`
  (`supervisor.go:389` `reloadRound`). `load` failing, panicking or overrunning rejects the config
  (Warn "reload rejected", no `runner` attr, Run keeps going; `loadConfig` at `supervisor.go:438`
  recovers a load panic itself and reports a plain error, not a `*PanicError`, because
  `*PanicError` names a Runner). Any `Reload` error/panic/overrun is fatal like a failed Start.
- `reloadInterrupted(ctx, runner, name)` (`supervisor.go:474`) mirrors `(*Supervisor).interrupted`
  for Start: a deadline between two Reloads returns `ErrReloadTimeout` naming no Runner while the
  log record names the Runner whose Reload is not called; Run's ctx being done returns nil.
- An abandoned Reload keeps its Runner in state `running`, so the stop phase calls Stop while the
  Reload may still run (the documented exception in `Reloader`'s doc, `reload.go`).
- Test gotcha: expiring the round from inside a Reload that then returns nil races, because the
  round's select sees the result and `roundCtx.Done()` ready together. `reload_test.go` uses
  `expiresOnInspection` (:480), which cancels on the first `Err()` read (the pre-offer check).
- `checkRecords` in `logging_test.go:83` requires a `runner` attr on every record; the round
  records ("reload rejected", "reload applied") carry none, so reload tests filter them.
- `launchRun` prepends `WithLogger(slog.New(slog.DiscardHandler))` (`supervisor_test.go:472`);
  a test calling `New` directly logs to `slog.Default()`.

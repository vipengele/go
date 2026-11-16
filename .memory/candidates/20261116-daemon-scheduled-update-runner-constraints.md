---
about: "What binds a scheduled self-update Runner in daemon: no update dependency or depguard allowance yet, no Logger/config injection into Runners, reload is SIGHUP-only and per-Runner (no atomic shared config), Source.Latest has no typed no-release error, apply is in update.Config.Apply not daemon, no jitter/scheduler precedent"
saw:
  - source/daemon/.golangci.yml
  - source/daemon/go.mod
  - source/daemon/runner.go
  - source/daemon/reload.go
  - source/daemon/clock.go
  - source/daemon/func.go
  - source/daemon/selfupdate/machine.go
  - source/update/source.go
  - source/update/github.go
  - source/update/apply.go
  - source/update/background.go
  - docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md
  - docs/adr/0025-a-daemon-reloads-its-config-on-sighup-through-an-opt-in-option.md
  - docs/adr/0028-lifecycle-time-and-readiness-are-injectable-seams.md
  - docs/adr/0033-a-daemon-self-update-restores-the-binary-before-recording-the-rollback.md
---

Checked 2026-11-16 while planning "Scheduled update runner".

- daemon does not require `go.vipengele.org/update` (go.mod:5-9; only cli/spec, cli/state, filelock,
  x/sys). `.golangci.yml` depguard allows the root package only cli/spec, cli/spec/paths, filelock;
  `selfupdate/**` additionally cli/state; `daemoncmd/**` is exempt. Neither lists `update`, so the
  runner needs a require + `replace ../update` (ADR-0006) and a new depguard allowance, or a new
  subpackage. ADR-0003 / ADR-0033 say daemon requires update and update never imports daemon.
- A Runner is handed no logger, config or clock: `Runner` has Name/Start/Done/Err/Stop only
  (runner.go:12-37). Logger is a Supervisor option (option.go:205 `WithLogger`, slog.Default at Run);
  Clock is `WithClock` on the Supervisor and `daemon.Clock` (Now, AfterFunc) which a runner must take
  by its own option, as `selfupdate.WithClock` does (selfupdate/options.go:21). Fake: daemontest.Clock
  (Advance, BlockUntilTimers). No synctest anywhere in daemon or update.
- Reload (ADR-0025, reload.go:19-47): SIGHUP only, never on Windows; one `load` per round offered to each
  `Reloader[T]` Runner in start order, one at a time; no shared/atomic config object, each Runner
  holds its own copy. Reload is never concurrent with that Runner's Start/Stop/Reload but runs
  concurrently with its work, so the Runner must guard its config. A Reload error/panic/overrun is
  FATAL to the run (not rolled back), so a runner must not return an error for a bad interval
  unless it wants shutdown. A failed load changes nothing, Warn logged by Supervisor.
- Release lookup: `update.Source.Latest(ctx, ch)` (source.go:8-12) returns an error for "no release";
  there is no exported sentinel. GitHubSource wraps unexported `errNoListedRelease` (github.go:294) for
  beta/edge; stable goes redirect then `/releases/latest` and a 404 surfaces as `*StatusError`
  (errors.go:25). A runner wanting "missing release at debug" has to match `*StatusError` 404 itself
  or the update project must export a sentinel.
- Apply is `update.Config.Apply(ctx, Selection)` (update/apply.go:72), returning `*Installed` that
  still holds the backup; the caller settles it. Needs `Config.Select(rel)` first (select.go:51).
  `selfupdate.Machine.Begin` records pending (machine.go:101); it is not called by Apply. Nothing in
  the repo yet joins Apply, Machine.Begin and `daemon.ErrRestartRequired` (errors.go:27); only
  testdata/winsvc uses it. Apply-exit-restart is the next story, not landed.
- Precedent: `update` background check (update/background.go:109 runCheck) is throttled by
  state `CheckInterval`, no jitter. Grep for jitter/schedul/synctest across docs, daemon, update: no
  hits; no abandoned attempt recorded.

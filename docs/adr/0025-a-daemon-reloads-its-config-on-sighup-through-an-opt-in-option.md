# A daemon reloads its config on SIGHUP through an opt-in option

Status: accepted. It amends
`0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md`, whose "signals are
the caller's" holds for SIGINT and SIGTERM, while SIGHUP belongs to the Supervisor of a daemon that
asks for reload. It fills the reload item that
`0023-a-daemons-single-instance-is-a-filelock-try-lock-with-a-holder-record.md` leaves out of
scope, and keeps to that ADR and to `0019-the-file-lock-is-its-own-project-filelock.md`: `daemon`
holds no `*_windows.go` file.

`daemon.WithReload[T](load)` is an `Option` of the Supervisor. Given it, `Run` subscribes to SIGHUP
and, on each one, calls `load` and offers the result to every running Runner that implements
`Reloader[T]`. Without it, `Run` touches no signal, so a program that does not ask for reload keeps
the operating system's default SIGHUP behaviour.

## The rules

- **Opt in twice.** The program opts in with `WithReload`; a Runner opts in by implementing
  `Reloader[T] interface { Reload(ctx context.Context, cfg T) error }`. A Runner that implements
  `Reloader` for another type is not offered the config.
- **The loader is a function.** `load` is a `func(context.Context) (T, error)`, so `daemon` depends
  on no config system. A program built on `cli/core` passes a closure over the `config` command
  group's `Options[T].Load`.
- **One reload round has one deadline.** `load` and every `Reload` of a round share one context
  that ends at the start timeout or when `Run`'s context ends, as the starts of a run share one.
  `Run` waits on each call against that context and abandons a call that outlives it.
- **A rejected config changes nothing.** When `load` returns an error or outlives the round's
  deadline, no Runner is called, the Runners keep the config they hold, and the Supervisor's logger
  records why at Warn. The daemon keeps running.
- **An accepted config is offered like a start.** Running Runners are offered it in start order. An
  error, a panic or a timeout of one `Reload` ends the run the way a failed `Start` does: every
  Runner is stopped in reverse order within the grace period and `Run` returns the error, wrapped
  with the Runner's name. A context error from a `Reload` while `Run`'s context is done is shutdown,
  not failure.
- **What a Runner can rely on.** `Reload` runs while the Runner's own work runs, so it synchronises
  with that work. It is never called concurrently with that Runner's `Start`, `Stop` or another
  `Reload`, with one exception: a `Reload` abandoned at the deadline may still be running when the
  stop phase calls `Stop`, as an abandoned `Start` may.
- **The subscription spans the run.** `Run` subscribes once the instance lock, if any, is held and
  before the first `Start`, and unsubscribes on every return path after the last `Stop`, so a SIGHUP
  during start or shutdown never terminates the process. One arriving before the wait loop is
  applied once the loop begins; one arriving during shutdown is dropped. SIGHUPs that arrive during
  a reload coalesce into one more reload.
- **Windows never reloads.** Windows delivers no SIGHUP. `WithReload` is accepted there and does
  nothing, so a cross-platform daemon keeps one code path.

## Considered options

- **`cli/core` owns SIGHUP**, as it owns SIGINT and SIGTERM, and hands it over on `spec.Env`.
  `cli/core` registers its signals for every program, and a CLI that caught SIGHUP would stop
  ending when its terminal closes; an opt-in in `cli/core` still leaves every daemon to wire the
  channel to the Supervisor itself.
- **Each program wires SIGHUP in its `main`** against a `Supervisor.Reload`. Every daemon would
  repeat the same signal handling, and its ordering against start and shutdown.
- **A Runner error that only logs.** Startup treats a Runner that cannot take its config as fatal;
  a reload that let a Runner continue on a config it refused would leave the daemon in a state no
  start can produce.

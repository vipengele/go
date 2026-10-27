# A Runner starts when ready and reports its exit through Done

Status: accepted. It supersedes the Runner contract of
`docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md`: the
blocking `Start`, the early return as the failure signal, the per-Runner start context cancelled
just before `Stop`, and the `Start` panic under a policy. Reverse stop order and one grace period
for the whole shutdown stand.

A `Start` that blocks for the Runner's whole life cannot say when the Runner is ready, so a
Runner started after it cannot rely on it, and a failure to start cannot be told apart from a
failure while running. `go.vipengele.org/daemon` splits the two: `Start` returns once the Runner
is ready, and `Done` and `Err` report the exit that follows.

## The rule

- **Contract.** A `Runner` has `Name`, `Start(ctx) error`, `Done() <-chan struct{}`, `Err() error`
  and `Stop(ctx) error`. `Start` returns once the Runner is ready; an error, or a panic, means it
  failed to start. `Done` closes once the Runner has exited, and `Err` then says why: nil, an
  error, or a `*PanicError`.
- **Order.** A Runner may have a method `Order() int`; without it its Order is 0. The Supervisor
  starts Runners in ascending Order, stably, so equal Orders start in the order given to `New`,
  and stops them in the reverse of the start order.
- **Start timeout.** Every `Start` shares one context, which carries the values and the
  cancellation of the context given to `Run` and is cancelled when the start phase ends. The
  whole phase is bounded by the start timeout, thirty seconds unless `WithStartTimeout` sets
  another; a `Start` still running when it expires is `ErrStartTimeout` naming the Runner.
- **Failed start.** A start failure, the start timeout or the cancellation of the run context
  during the phase calls no further `Start` and stops the Runners already started, in reverse.
- **Stop.** `Stop` is called exactly once on every Runner whose `Start` was called, including one
  whose `Start` failed, panicked or has not returned. After `Stop` returns, the Supervisor waits
  for `Done` within the same grace period: the return of `Stop` alone is never taken as the
  Runner having exited.
- **Exit.** A Runner that exits before shutdown is the failure that begins it:
  `ErrRunnerExited` when `Err` is nil, otherwise `Err` naming the Runner. During shutdown an
  `Err` wrapping `context.Canceled` reports only that the Runner was stopped and is dropped; any
  other `Err` is a stop problem.
- **Panics.** A panic in `Start` is always a start failure. `PanicIsolate` applies only to a
  Runner that has started and exits with a `*PanicError`; `PanicFail` stays the default.
- **`Func`.** `daemon.Func(name, run, opts...)` adapts a function to a Runner. `Start` launches
  `run` in a goroutine and returns at once; `run` gets `context.WithoutCancel` of the start
  context, so it outlives the start phase, and `Stop` cancels it. `WithOrder` places it.
- **HTTP.** `NewHTTPServer(srv)` binds `srv.Addr` in `Start`, so a failure to bind is a start
  failure; `NewHTTPServerListener(srv, l)` serves a listener the caller bound. Both have Order
  `DefaultHTTPOrder`, 100, so the server starts after and stops before the Runners at the default
  Order. `Start` overrides `srv.BaseContext` with a context free of the start context's
  cancellation, cancelled only once the drain ends: `Stop` calls `Shutdown`, and handlers in
  flight get the grace period rather than being cancelled when shutdown begins. When the grace
  ends first, `Stop` closes the server.
- **Restart.** `ErrRestartRequired` is what a Runner returns to ask for the process to be
  restarted. `RestartRequested(err)` is true when every leaf of `err` is or wraps it, and false
  for any error holding a `*PanicError`: a panic is never a restart. The consumer maps a requested
  restart to exit status 0, so a service manager relaunches the process without recording a
  failure.

## Considered options

- **A `Daemon` wrapper type around the Supervisor.** A second type with the same lifecycle gives
  a consumer two names for one thing and nothing to choose between.
- **`Serve` methods on the Supervisor.** They tie the Supervisor to HTTP; a Runner adapter keeps
  the Supervisor ignorant of what its Runners do, and a daemon may serve nothing.
- **A blocking `Start` with a ready callback.** A Runner that forgets to call it hangs the start
  phase until the timeout, and one that calls it twice or after failing needs rules of its own.
  A return from `Start` cannot be forgotten.
- **A `Ready` channel on the Runner.** It is a second way to say what the return of `Start`
  already says, and a Runner whose `Start` fails would have to leave it open while also
  reporting the failure.
- **Plain `errors.Is(err, ErrRestartRequired)` for a restart.** It is true for a restart joined
  with a stop timeout or a panic, which then exits 0 and hides the failure from the service
  manager.
- **A grace period per Runner for the HTTP drain.** It reopens the N-times-grace worst case that
  ADR-0014 rejects; the drain takes what is left of the one grace period.

## Out of scope

- **Signals** stay the caller's (`docs/adr/0012-signals-are-registered-by-execute-and-injected-through-env.md`):
  the context given to `Run` drives shutdown.
- **Exit codes** are `cli/core`'s
  (`docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md`);
  the package only says whether an error is a requested restart.
- **Health**, **HTTP hardening** (timeouts, header limits, TLS), and **systemd readiness
  notification and socket activation**.

## Consequences

- **A later Runner can rely on an earlier one.** It starts only once the earlier one is ready.
- **A Runner owns its goroutine.** `Start` launches the work and returns; `Func` and the HTTP
  Runner do that for the common cases.
- **A restart can still exit non-zero.** A Runner that exits during shutdown with an error other
  than `context.Canceled` joins that error to the restart, so `RestartRequested` is false and the
  process exits non-zero. A service manager with `Restart=always` relaunches it either way; the
  journal records a failure, which is what that error is.
- **Start is bounded as well as stop.** A hung `Start` costs the start timeout, then the grace
  period, and is reported.

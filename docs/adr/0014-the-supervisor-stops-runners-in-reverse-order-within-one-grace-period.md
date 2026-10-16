# The supervisor stops runners in reverse order within one grace period

Status: accepted.

A daemon runs several pieces of long-running work, and some depend on others: a server on the
database it queries. `go.vipengele.org/daemon` starts them as **Runners** under a **Supervisor**
and, on shutdown, stops them in the reverse of the order they were given, all within one **Grace
period**. Lifecycle `Serve` and health, which the CONTEXT.md **Supervisor** definition mentions,
are later stories and not part of this decision.

## The rule

- **Package.** The root package `daemon` exports `Runner` and `Supervisor`, built with
  `daemon.New(runners, opts...)` and run with `Run(ctx)`.
- **Early return.** A `Start` that returns before its context is cancelled has failed, with nil
  as well: `ErrRunnerExited`. It begins the shutdown.
- **Errors.** `Run` returns `errors.Join` of the failure that began the shutdown, the isolated
  Start panics in start order and the stop problems in stop order. An error that only reports the
  cancellation of a Start context is dropped.
- **Panics.** A panic in `Start` or `Stop` is recovered into a `*PanicError` and never
  re-raised. `WithPanicPolicy` chooses between `PanicFail`, the zero value, which treats a
  panicking `Start` as a failure that stops the rest, and `PanicIsolate`, which records it while
  the siblings keep running. `PanicFail` is the default deliberately: a zero-config daemon should
  not run silently degraded.
- **Grace.** One deadline, ten seconds unless `WithGrace` sets another, made from
  `context.Background()` and never from the cancelled run context, covers the whole shutdown.
- **Order.** Each Runner's `Start` runs under its own context, which carries the values of the
  parent but not its cancellation. Shutdown cancels it just before calling `Stop`, so a Runner
  started earlier keeps running until its turn. `Stop` runs in its own goroutine and is selected
  against the deadline.
- **Stop errors.** `Stop` returns an error, so a failing `Stop` is reported.

## Considered options

- **The API in a subpackage, `daemon/runtime` or `daemon/supervisor`.** A consumer imports the
  package for exactly these two types, and a second path adds a hop with nothing to separate.
- **An early nil from `Start` as a clean finish.** A Runner that returns while the daemon is
  meant to run has stopped doing its work, and a daemon that carries on without it is degraded
  without anyone being told.
- **Reporting only the first failure.** The stop problems are what an operator needs to see after
  a failed run, and dropping them hides a Runner that did not release its resources.
- **Joining every error unordered.** The failure that began the shutdown is the cause and the
  rest follow from it; an unordered join buries it.
- **Re-raising the panic after shutdown.** It replaces the orderly result with a crash and
  loses the other Runners' errors; a `*PanicError` carries the value and the stack.
- **Always failing or always isolating, with no option.** Each is right for some daemons: a
  worker pool may outlive one task, a server may not outlive its database.
- **A grace period per Runner.** The worst case is N times the grace, past the kill timeout of a
  service manager, which then ends the process without the remaining Runners having stopped.
- **Concurrent `Stop`.** It ignores the order in which the Runners depend on each other.
- **One shared run context, cancelled at once.** Every `Start` would see its cancellation at the
  same moment, so the reverse order would govern only `Stop`.
- **Waiting on every `Start` with no timeout.** A hung Runner hangs the process.

## Consequences

- **Dependency order holds through shutdown.** A Runner is cancelled and stopped before the
  Runners started earlier than it.
- **Shutdown is bounded.** It takes the grace period at most, plus the time to report.
- **An abandoned `Stop` or `Start` may still be running when `Run` returns.** A Runner that
  cannot be stopped is reported as `ErrStopTimeout` and left behind, not waited for.
- **Signals are the caller's.** The package handles none; the context given to `Run` drives it.

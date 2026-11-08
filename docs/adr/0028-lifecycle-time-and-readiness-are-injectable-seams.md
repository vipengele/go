# Lifecycle time and readiness are injectable seams

Status: accepted. It extends
`docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md` and
`docs/adr/0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md`, whose start
timeout, grace period, per-check timeout and uptime it leaves unchanged and makes testable from
outside the package.

A Supervisor's deadlines and Health's timings are measured against a clock, and a Supervisor has a
moment at which every Runner is up and it begins waiting. A test of a start timeout, a stop grace, a
reload round or a check timeout either waits real wall time or reaches into unexported state, and a
package outside `go.vipengele.org/daemon` can do neither. The package therefore exports two seams:
a `Clock` and a readiness callback. They exist so that a public test kit,
`go.vipengele.org/daemon/daemontest`, with a fake `Clock`, a fake `Runner` and a helper that starts
a Supervisor on `127.0.0.1:0`, can be built on exported API only.

## The rule

- **Clock.** `daemon.Clock` is an interface of two methods:

  ```go
  type Clock interface {
      Now() time.Time
      AfterFunc(d time.Duration, f func()) (stop func() bool)
  }
  ```

  `AfterFunc` runs `f` once `d` has elapsed on the clock, and `stop` reports whether it prevented
  that, as `time.Timer.Stop` does.
- **Options.** `WithClock(Clock) Option` sets the Supervisor's clock; `WithClock(nil)` is an error
  from `New`. `WithHealthClock(Clock) HealthOption` sets Health's clock; a `HealthOption` has no
  error return, so nil means the real clock. Without either option the clock is the real one.
- **One deadline constructor.** An unexported `withDeadline(clock, parent, d)` builds every
  deadline. With the real clock it is exactly `context.WithTimeout`. With any other clock it returns
  a `context.Context` of its own:
  - `Deadline()` is `clock.Now().Add(d)`, or the parent's deadline when that is earlier;
  - `Err()` is `context.DeadlineExceeded` once the clock's timer has fired and the parent's error
    once the parent has ended;
  - `Done` closes on either;
  - `Value` delegates to the parent;
  - the cancel function stops the armed timer, and the context is safe whichever of the timer, the
    parent and the cancel function comes first.
- **Where it applies.** The Supervisor's start-timeout, stop-grace and reload-round contexts, and
  Health's `StartedAt`, `Uptime`, check `Duration` and per-check timeout.
- **Check timeouts.** Health decides that a check timed out itself: the check's context reports
  `context.DeadlineExceeded` while the caller's context is live, and the result wraps
  `ErrCheckTimeout`. It does not read `context.Cause`, which a custom context cannot carry.
- **Not covered.** The request-log timing of `httpserver` stays on `time.Now`. It measures the wall
  time of real requests, and no test needs to fake it.
- **Readiness.** `WithReady(func()) Option` registers a callback; `WithReady(nil)` is an error from
  `New`. It fires once per `Run`, at the first entry into the Supervisor's waiting loop, which is
  after every Runner's `Start` has returned. A once-guard stops it firing again when the loop is
  re-entered after a reload round or after an isolated panic exit, and it never fires when the start
  phase fails.
- **Synchronous.** The callback runs on `Run`'s goroutine, so a callback that blocks blocks `Run`:
  the Supervisor does not enter its waiting loop, and so does not notice a Runner exiting or the
  cancellation of the run context, until it returns. A callback that has work to do starts its own
  goroutine. The same seam serves a notification of a service manager, such as `sd_notify` with
  `READY=1`, which must be sent exactly when the Supervisor is up.

## Considered options

- **Plain `context.WithTimeout` everywhere, with no clock.** A test of the start timeout, the stop
  grace or the reload timeout waits that much real time, which is slow, and a margin small enough to
  be fast is flaky on a loaded machine.
- **Expire-only hooks.** A test hook that forces a deadline to expire cannot express `Deadline()`,
  which a Runner is promised it can read as its start deadline (`source/daemon/doc.go`), nor the
  `Now`-based values of Health such as uptime and check duration, nor the order in which several
  timers fire.
- **A `context.WithCancelCause` context cancelled from `AfterFunc`.** It has no `Deadline()`, and a
  Runner that sees no deadline on its start context loses the promise above.
- **A sentinel Runner at `Order` `math.MaxInt` whose `Start` signals readiness.** It appears in the
  Runner lists, in Health and in the calls a test records, and takes a stop slot of its own. It
  competes with a user Runner that also chooses `math.MaxInt`, and it cannot tell the start of the
  last Runner from the waiting loop having been entered.

## Consequences

- **A test controls time.** A fake `Clock` advances a start timeout, a stop grace, a reload round or
  a check timeout without waiting, and the contexts report the deadline a Runner would see.
- **A custom context is code to keep correct.** `withDeadline` has to stay race-safe for every order
  of timer, parent and cancel, and carries its own tests; with the real clock it adds nothing over
  `context.WithTimeout`.
- **A blocking `WithReady` callback blocks `Run`.** It is the caller's to keep short.
- **`daemontest` needs no access to unexported state.** The seams are the public API, and a change
  to them is a change to that API.

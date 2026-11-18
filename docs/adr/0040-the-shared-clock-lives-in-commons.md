# The shared clock lives in commons

Status: accepted. It relates to
`docs/adr/0028-lifecycle-time-and-readiness-are-injectable-seams.md`, whose `daemon.Clock` it
does not replace.

Code in several projects measures time and waits: `daemon` against deadlines and uptime, and the
resilience policies of the platform between retries and while waiting for a permit. A test of any of
them either waits real wall time or reaches into unexported state. A clock that every such project
can require, and a test clock that moves only when the test says so, make those tests fast and exact.

## The rule

- **`commons/clock`** holds `Clock`, an interface of three methods:

  ```go
  type Clock interface {
      Now() time.Time
      Sleep(ctx context.Context, d time.Duration) error
      AfterFunc(d time.Duration, f func()) (stop func() bool)
  }
  ```

  `AfterFunc` and its `stop` behave as `time.AfterFunc` and `time.Timer.Stop` do.
- **`Sleep` returns an error.** It returns `ctx.Err()` when the context ends before `d` has
  elapsed, including when it has already ended, and nil otherwise. It exists because a resilience
  policy waits between retries and for a permit, and that wait has to end with the caller's context.
- **`clock.System()`** returns the clock of the operating system. Every call returns an equal value.
- **`clock.IsSystem(c)`** reports whether `c` is that clock. Code that holds a `Clock` uses it to
  take the path the standard library already provides, `context.WithTimeout`, only when the clock
  is the real one, as `daemon` does for its deadlines (ADR-0028). With any other clock it builds
  the deadline from `Now` and `AfterFunc`.
- **`commons/clock/clocktest`** holds `Manual`, a `Clock` whose time moves only through `Advance`.
  `New` sets its start time, `Advance` fires every sleep and deferred call that falls due, in
  deadline order, and `Pending` counts those still waiting, so a test of code that sleeps in
  another goroutine waits for the sleep to register before it advances.
- **`Manual.AfterFunc` runs `f` synchronously inside `Advance`,** not in a goroutine of its own. A
  test sees every effect of a deferred call when `Advance` returns, with nothing to wait for. `f`
  must not call `Advance`.
- **`commons` stays free of dependencies** beyond the standard library, so any project may require it.

## Considered options

- **A clock per project,** as `daemon` has (`source/daemon/clock.go`). Each project that needs one
  declares the same interface and ships its own test clock, and a consumer wiring `daemon` and a
  resilience policy into one test builds two fake clocks that cannot be advanced together.
  Rejected: one clock is passed to every project.
- **A clock inside `resilience`.** The clock is not a resilience concern, and `daemon` would require
  a resilience project to read the time. Rejected: `commons` is the project every other may
  require.
- **No `IsSystem`,** leaving a holder of a `Clock` to type-assert or to compare with `System()`.
  Rejected: the real clock is a private type, so there is one place that knows how to recognise it.

## Consequences

- **A test controls time.** A `Manual` clock ends a sleep, fires a deferred call or expires a
  deadline without time passing.
- **`daemon` keeps its own `Clock` interface.** Its `Now` and `AfterFunc` are a subset of the
  shared one, so a `clock.Clock` satisfies `daemon.Clock`. Replacing the `daemon` type with the
  shared one is work of its own, outside this decision.
- **`Manual` departs from the interface contract in one respect,** the synchronous `AfterFunc`,
  which only a test of code that does not rely on the call running in another goroutine can assume.

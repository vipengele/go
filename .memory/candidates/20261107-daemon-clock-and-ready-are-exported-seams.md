---
about: "daemon's lifecycle time and readiness are exported seams (Clock, WithClock, WithHealthClock, WithReady); a non-real clock gets a custom deadline context that carries no cause"
saw:
  - source/daemon/clock.go
  - source/daemon/option.go
  - source/daemon/health.go
  - source/daemon/supervisor.go
  - docs/adr/0028-lifecycle-time-and-readiness-are-injectable-seams.md
---

- `daemon.Clock` (Now, AfterFunc) drives the Supervisor's start, stop and reload contexts
  (supervisor.go, `newStartContext`/`newStopContext` built through `withDeadline`) and Health's
  StartedAt, Uptime, check Duration and per-check timeout (health.go, `run`).
- `withDeadline` (clock.go) is exactly `context.WithTimeout` for `realClock{}`; for any other clock
  it returns `clockContext`, whose Deadline() is clock.Now()+d (or the parent's earlier one) and
  whose Err() is DeadlineExceeded on fire. It carries no cause: `context.Cause` on it walks to the
  parent and can be nil after expiry. Health therefore decides a check timeout itself
  (`endedErr`: check ctx DeadlineExceeded while the caller's ctx is live) and reads Cause only
  from the caller's ctx.
- `WithReady` is called in `wait()` once, outside the waiting loop and before the first `waiting()`
  hook, so reload rounds and PanicIsolate re-entries never refire it; it runs on Run's goroutine.
- The unexported seams (newStartContext, newStopContext, newReloadContext, notify, waiting) remain
  for in-package tests; daemon's own tests keep their own fakes (`scriptedRunner`, `manualClock` in
  clock_test.go) because importing daemontest from package daemon is a cycle.

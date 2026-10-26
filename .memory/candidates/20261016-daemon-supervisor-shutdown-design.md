---
about: "Why daemon's Supervisor builds its grace deadline from context.Background, shares one start context across every Start, and waits for Done after Stop returns"
saw:
  - source/daemon/supervisor.go
  - source/daemon/supervisor_test.go
  - docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md
  - docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md
---

- The stop context comes from `Supervisor.newStopContext`, defaulting to `context.WithTimeout(context.Background(), grace)` (supervisor.go, `New`). It must not derive from the run context, which is already cancelled when shutdown begins and would be born expired. Tests replace the seam with a context they cancel by hand, so grace paths run with no clock and no `time.Sleep`. `newStartContext` is the same kind of seam for the start timeout.
- Every `Start` shares one context from `newStartContext(parent)`: the parent's values and cancellation, bounded by the start timeout, cancelled when the start phase ends (`Supervisor.start`). A runner's long-lived work must not be bound to it; `Func` and the HTTP runner derive theirs with `context.WithoutCancel(startCtx)`.
- `launched.stop` calls `Stop` in its own goroutine selected against the shared deadline, so a hung Stop cannot hold `Run` past the grace; the goroutine is abandoned. Once the deadline has passed every remaining Runner is reported `ErrStopTimeout` without being waited on. After `Stop` returns, `launched.finish` also waits for `Done()` within the same grace: Stop's return alone is never the join (ADR-0021).
- Under `PanicIsolate`, `Supervisor.wait` keeps blocking after every running Runner has exited with a `*PanicError`; only the parent context or a non-panic exit begins shutdown. `supervisor_test.go` observes that `Run` is waiting again through the `Supervisor.waiting` hook, which `wait` calls once per loop pass.

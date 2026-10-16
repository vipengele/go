---
about: "Why daemon's Supervisor cancels per-runner contexts that do not inherit the parent's cancellation, and why its grace deadline is built from context.Background"
saw:
  - source/daemon/supervisor.go
  - source/daemon/supervisor_test.go
  - docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md
---

- `launch` in `source/daemon/supervisor.go` gives each Runner's Start `context.WithCancel(context.WithoutCancel(parent))`. If Start inherited the parent's cancellation, cancelling the parent would stop every Runner at once and the reverse stop order would govern only `Stop`. `Run` watches the parent itself and then cancels each Start context just before calling that Runner's Stop.
- The stop context comes from `Supervisor.newStopContext`, defaulting to `context.WithTimeout(context.Background(), grace)`. It must not derive from the run context, which is already cancelled when shutdown begins and would be born expired. Tests replace the seam with a context they cancel by hand, so grace paths run with no clock and no `time.Sleep`.
- `Stop` runs in its own goroutine selected against the shared deadline, so a hung Stop or Start cannot hold `Run` past the grace; the goroutine is abandoned and documented as such. Once the deadline has passed every remaining Runner is reported as `ErrStopTimeout` without being waited on.
- Under `PanicIsolate`, `wait` keeps blocking after every Start has panicked; only the parent context or a non-panic early return begins shutdown. `supervisor_test.go` observes that `Run` is waiting again through a `Done()` call count on a wrapped context, which depends on `wait` calling `ctx.Done()` once per pass.

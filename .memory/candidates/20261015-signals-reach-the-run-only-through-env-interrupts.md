---
about: "OS signals are registered only in executeProcess and reach the run as values on spec.Env.Interrupts; ExecuteArgs registers nothing, a nil channel keeps the run inline, and a closed channel must be set to nil in the watcher or it busy-spins"
saw:
  - source/cli/core/execute.go
  - source/cli/spec/env.go
  - source/cli/core/execute_test.go
  - docs/adr/0012-signals-are-registered-by-execute-and-injected-through-env.md
---

- `signal.Notify` is called once per process run in `executeProcess` (`notifyInterrupts`: SIGINT and
  SIGTERM into a two-slot channel, one forwarder goroutine, `signal.Stop` before the forwarder is
  joined). `ExecuteArgs` touches only what `env` gives it and runs concurrently on one tree, so it
  must never call `signal.Notify`.
- The seam is `spec.Env.Interrupts <-chan struct{}`, not `os.Signal`: `cli/spec` must not import `os`
  (enforced by `TestPackageImportsNoCLIFramework`). `Env.Resolved` copies it unchanged; nil stays nil.
- Nil `Interrupts`: the run is inline on the calling goroutine, no extra goroutine
  (`TestExecuteArgsWithoutInterrupts` counts goroutines). Non-nil: a watcher goroutine plus a run
  goroutine. A run panic is recovered in the run goroutine and re-panicked on the caller's.
- `watchInterrupts` sets its local `interrupts = nil` when the channel is closed. Without it the
  `select` receives from the closed channel on every iteration and the watcher busy-spins; deleting
  the line breaks no test (`TestExecuteArgsClosedInterruptsDeliverNoInterrupt` covers only the
  semantics of a closed channel, not the spin).
- The second interrupt returns 130 from `executeArgsCtx` without waiting; the abandoned run goroutine
  may still write to `env` streams and run PostRun hooks. Library code never calls `os.Exit`.

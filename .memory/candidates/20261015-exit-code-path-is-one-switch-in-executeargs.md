---
about: "exit codes are chosen by one precedence in executeArgsCtx/report/exitCode, rule 1 reads the run's context so an interrupt-cancelled run and a cancelled parent context both give a silent 130; the post-run HookError is deliberately opaque to the rules"
saw:
  - source/cli/core/execute.go
  - source/cli/core/exitcodes.go
  - source/cli/core/app.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/spec/exit.go
  - docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md
  - docs/adr/0012-signals-are-registered-by-execute-and-injected-through-env.md
---

- `report` (`execute.go`) is the single place an error becomes an exit code and a stderr line, for
  `ExecuteArgs`, `App.ExecuteArgs` and the process entry points alike. Rule 1 is
  `runCtx.Err() != nil && errors.Is(err, context.Canceled)` -> `ExitInterrupted`, nothing printed. It
  reads `runCtx`, the context derived from the caller's, so a cancelled parent and a first interrupt
  (`Env.Interrupts`) take the same path. `spec.ReportedError` suppresses only the stderr line; the
  code then comes from `exitCode`: registrable `spec.CodedError`, the App's `exitMatchers`,
  `*cobra.UsageError` -> 2, else 1.
- An interrupt changes no other rule: a plain error after an interrupt is still 1 and printed, a
  usage error 2, a coded error its code. Only the second interrupt bypasses the rules, returning
  130 at once from `executeArgsCtx` without waiting for the run.
- `HookError` (`internal/cobra/cobra.go`) has no `Unwrap` on purpose: a `Coded`, registered or
  `Reported` error inside a failed post-run hook is opaque and exits 1, interrupted or not. A pre-run
  hook error is returned raw and maps like a run error.
- `Coded`/`Reported` live in `cli/spec` (`exit.go`) because `update` and `daemon` import spec only; the
  registry is per-App state in `settings` (`gochecknoglobals` forbids a package-level one) and
  `WithExitCode` panics inside `New` for 0, 2, 130 and anything outside 1..125.

---
about: "exit codes are chosen in executeArgsCtx/exitCode by a five-rule precedence; the post-run HookError is deliberately opaque to it, and the public entry points run with context.Background() so 130 is unreachable through them"
saw:
  - source/cli/core/execute.go
  - source/cli/core/exitcodes.go
  - source/cli/core/app.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/spec/exit.go
  - docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md
---

- `executeArgsCtx` (`execute.go`) is the single place an error becomes an exit code and a stderr line.
  The interrupt rule (root `ctx.Err() != nil` and `context.Canceled` -> `ExitInterrupted`, nothing
  printed) runs before the `<root>: <err>` line is written; `spec.ReportedError` suppresses only that
  line; the code then comes from `exitCode`: `spec.CodedError` (registrable only), the App's
  `exitMatchers`, `*cobra.UsageError` -> 2, else 1.
- `App.ExecuteArgs` and the package-level `executeArgs` both pass `context.Background()`, so rule 1 is
  reachable only through `ExecuteArgsContext` in `export_test.go`. A public entry point that should
  return 130 needs a cancellable ctx threaded in there.
- `HookError` (`internal/cobra/cobra.go`) has no `Unwrap` on purpose: a `Coded`, registered or
  `Reported` error inside a failed post-run hook is opaque and exits 1. A pre-run hook error is returned
  raw and maps like a run error.
- `Coded`/`Reported` live in `cli/spec` (`exit.go`) because `update` and `daemon` import spec only; the
  registry is per-App state in `settings` (`gochecknoglobals` forbids a package-level one) and
  `WithExitCode` panics inside `New` for 0, 2, 130 and anything outside 1..125.

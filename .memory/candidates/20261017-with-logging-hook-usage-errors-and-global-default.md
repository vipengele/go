---
about: "WithLogging's pre-run hook reports flag misuse as the internal *cobra.UsageError, not spec.UsageError, and installs the process-wide slog default, so overlapping executions share the last logger"
saw:
  - source/cli/core/logging.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/spec/usage.go
  - source/cli/core/execute.go
  - source/cli/core/app.go
  - source/cli/core/logging/logging.go
---

Found while implementing the opt-in slog setup (ADR-0018).

- `spec.UsageError{Question, Flag}` is for a run function that needs a prompt: its `Error()` is always
  "<Question> requires interactive input[; pass <Flag>]". Returning it for a flag conflict prints a wrong
  sentence. The `WithLogging` hook (`logging.go`, `loggingFlags.hook`) returns `&cobra.UsageError{Path, Err}`
  instead; `internal/cobra` is importable from `cli/core`, and `exitCode` rule 4 in `execute.go` maps it to
  exit 2 even when it comes out of a pre-run hook (the wrapper that stops hook errors being taken for usage
  errors does not apply to this one; pinned by `hooks_test.go` and `TestWithLoggingUsageErrors`).
- `logging.Setup` (`logging/logging.go`) calls `slog.SetDefault`. Executions that overlap in one process share
  the logger installed last, whose writer is that execution's `Env.Stderr`; `cli.LoggerFrom(ctx)` is the
  per-execution handle. `App`'s "runs concurrently" promise in `app.go` carries that caveat for `WithLogging`.
- Tests that execute a `WithLogging` App or call `Setup` must not use `t.Parallel` and must restore
  `slog.Default()` in `t.Cleanup`.
- The `WithLogging` prerequisite check (`loggingProblem`, run at the top of `ExecuteArgs`) reports before tree
  validation, so a missing `WithVerbose`/`WithQuiet` hides other tree problems on that run.

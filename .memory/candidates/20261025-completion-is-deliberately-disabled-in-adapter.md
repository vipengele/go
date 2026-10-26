---
about: "cobra's completion command is off in the cli/core adapter unless an App passes WithCompletion; the help subcommand is always a hidden stub; flags have no enum or path kind"
saw:
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/app.go
  - source/cli/core/execute.go
  - source/cli/core/execute_test.go
  - source/cli/spec/flag.go
---

- `internal/cobra/cobra.go:111-112`: `DisableDefaultCmd = !cfg.Completion` and `SetHelpCommand(&spf.Command{Hidden: true})`. `Config.Completion` is set only by `WithCompletion()` (`app.go:120`, passed at `app.go:237`); the package-level `Execute`/`ExecuteArgs` build `Config` at its zero value, so they never have `completion` (pinned by `TestExecuteArgsNeverHasCompletion` and `TestExecuteNeverHasCompletion`, `execute_test.go`).
- Without the option `completion` and `completion bash` are `UsageError` "unknown command" (exit 2), and root `--help` omits it (`app_test.go` `TestAppCompletionNeedsTheOption`). With it, `completion <shell>` is cobra's own command, outside `runHooked`, so it needs no `SkipConfig`/`SkipUpdateCheck`.
- Cobra's `completion bash x` exits 1, not 2: its error is not wrapped as a `UsageError`. `completion` and `completion bogus` print help and exit 0.
- The help subcommand has no name, so `tool help` is unknown and the stub is invisible. It is also why `__complete ""` prints a blank first candidate (cobra lists `finalCmd.helpCommand`); bash and zsh scripts skip blank lines, fish and powershell are untested.
- Flags have only Bool/String/Int kinds (`spec/flag.go`); there is no enum kind. Completion is attached with `spec.Complete(...)` and does not affect parsing, so enum-ish flags (`--output text|json`, `--log-format`) are still validated by hand in cli/core.
- ADR-0020 records the opt-in; only bash is installed on the dev box, so the zsh, fish and pwsh load steps of `completion_shells_test.go` skip there.

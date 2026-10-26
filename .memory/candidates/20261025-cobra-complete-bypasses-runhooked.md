---
about: "cobra's hidden __complete is live in every cli/core App whether or not WithCompletion is set, and bypasses every hook, so a CompleteFunc must close over what it needs"
saw:
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/app.go
  - source/cli/core/app_test.go
  - source/cli/core/internal/cobra/cobra_test.go
  - source/cli/spec/command.go
---

- `DisableDefaultCmd` drops only `completion` (`cobra.go:111`); cobra still adds hidden `__complete`/`__completeNoDesc` in `ExecuteC`. Its Run is cobra's own, so `runHooked` is never reached: PreRun, PostRun, the config hook, the logging hook, the update check and the Output resolver are all skipped. `app_test.go` `TestAppCompletesWithoutTheOptionAndRunsNoHook` pins it with a broken `config.yaml` and a failing hook; `cobra_test.go` `TestRunCompletionRunsNoHook` pins it in the adapter.
- A `spec.CompletionFunc` therefore sees `cmd.Context()` or `context.Background()`, with no config, logger or `Invocation`. It closes over what it needs. `__complete` writes cobra's `Completion ended with directive: ...` line to stderr.
- Flag completion funcs are registered with `RegisterFlagCompletionFunc` only when `args[0]` is `__complete`/`__completeNoDesc` (`completing`, `cobra.go:128`): cobra keeps them in a process-wide map keyed by `*pflag.Flag` that it never clears, and `Run` builds new flags every execution. `ValidArgsFunction` holds no global state and is set whenever the command has a `Run` (`cobra.go:170`).
- An `ArgCompletion` on a command with no `Run` is ignored by the adapter, and `Command.Validate` (`spec/command.go`, beside the `Args.Validate()` call) rejects it, as it does one on a command whose `Args` accepts none, and `Complete(...)` on a bool flag.
- The positional completion proposes nothing once the command line holds the maximum number of args `Args` accepts.

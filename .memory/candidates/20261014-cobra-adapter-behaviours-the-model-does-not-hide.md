---
about: "internal/cobra must work around cobra: nil args fall back to os.Args, a Run-less command never checks its args, and a user -h shorthand panics against the help flag"
saw:
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/execute.go
  - source/cli/core/execute_test.go
  - source/cli/spec/command.go
---

Facts the adapter encodes, each of which breaks silently or panics if the workaround is removed:

- `cobra.Command.SetArgs(nil)` makes cobra read `os.Args[1:]`, so `ExecuteArgs(env, root, nil)` would run the
  process's own arguments. `internal/cobra` passes `[]string{}` for a nil slice.
- Cobra shows help for a command with no run function and never evaluates its `Args`, so an unknown
  subcommand would exit 0. A `spec.Command` with no `Run` gets its own `Args` check and a `RunE` that prints
  help; any argument is a usage error (exit 2).
- Cobra gives the help flag the `-h` shorthand, and pflag panics when a user flag already uses `-h`.
  The adapter declares the help flag itself and drops `-h` when a visible flag has shorthand `h`.
  A flag named `help` is refused at run time (exit 1); `spec.Command.Validate` does not reserve it.
- Usage errors (exit 2) are detected by a typed `UsageError` produced by the adapter's own
  `PositionalArgs` and `SetFlagErrorFunc`, never by matching cobra's message text.
- `Execute` reads the real process, so `execute_test.go` re-executes the test binary as a child process
  (TestMain) to check streams, working directory, `os.LookupEnv` and exit code behaviourally.

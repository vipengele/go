---
about: "the error-to-exit-code path is a single errors.As switch in executeArgs over a context.Background() run; no ADR or note covers errors, exit codes or signals, and exit codes are ADR-0003 cli-owned"
saw:
  - source/cli/core/execute.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/hooks.go
  - source/cli/core/app.go
  - source/cli/core/execute_test.go
  - source/cli/core/.golangci.yml
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
---

Found while planning issue #25 (error to exit-code registry). Evidence is reading plus
`grep -rni "exit\|signal\|SIGINT\|Canceled"` over source/cli and docs (no hits beyond those below).

- Exit codes are consts ExitOK/ExitError/ExitUsage = 0/1/2 (`execute.go:14-24`); 130 does not exist.
- The whole mapping is `executeArgs` (`execute.go:92-108`): Validate failure -> ExitError; `cobra.Run`
  error is written to env.Stderr as `<root name>: <err>` BEFORE the code is chosen (`:102`), then
  `errors.As(*cobra.UsageError)` -> ExitUsage, else ExitError. A registry or a "suppress generic
  message" error must hook in before `:102`.
- The context is hard-coded `context.Background()` (`execute.go:98`); run functions and hooks never see
  cancellation. Signals (#27) need a ctx parameter threaded through `executeArgs` and `App.ExecuteArgs`
  (`app.go:114-120`).
- Usage errors are the typed `cobra.UsageError` (`internal/cobra/cobra.go:26-45`), which lives in an
  internal package, so `cli/core` can see it but a consumer cannot. It is produced at: SetFlagErrorFunc
  (`:100`), Args policy (`:179`), unknownCommand (`:229`), and an Output resolver error (`:190`, e.g.
  -v with -q). It has Unwrap; `HookError` (`:70-78`) deliberately has none, so a post-run hook error is
  never exit 2 (documented `hooks.go:14`, `:21`). A registry that walks the chain with errors.As/Is must
  keep this: HookError hides the wrapped error from any registry lookup too.
- The `read()` error in RunE (`cobra.go:182-185`) is returned bare, so it maps to ExitError, not usage.
- No ADR mentions errors, exit codes, usage errors or signals; ADR-0003 only lists "exit codes" among
  what `cli` (the app shell) owns (line 17). ADR-0005/0009 fix that `cli/spec` imports no framework and
  commands are immutable data; `update`/`daemon` depend on `cli/spec` only, so a registry type that
  contributed commands must raise errors for must live in `cli/spec`, while cobra-aware mapping stays in
  `cli/core`. Registration as an `App` option fits ADR-0009 (hooks are App options, never Command fields).
- `gochecknoglobals` is enabled in `source/cli/core/.golangci.yml`, so a package-level registry var is a
  lint failure; the registry must be per-App state in `settings` (`app.go:35-48`), shared read-only by
  concurrent executions (`App` runs concurrently, `execute_test.go:406`).
- Tests that pin the current path: `TestExecuteArgsExitCodes` (`execute_test.go:134`), invalid tree
  (`:302`), the TestMain child-process test for `Execute` (`:427-497`), and the hooks tests; 91
  ExitOK/Error/Usage references in `source/cli/core/*_test.go`. No golden files exist and nothing else
  in the repo calls `ExecuteArgs`/`Execute`.
- Not tried/abandoned: nothing in the store or ADRs records an earlier attempt.

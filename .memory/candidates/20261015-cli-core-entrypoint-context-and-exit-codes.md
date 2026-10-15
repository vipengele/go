---
about: "cli/core ExecuteArgs hard-codes context.Background(), maps only UsageError to 2 and everything else to 1; no signal, cancellation or 130 handling exists, and no ADR or note covers it"
saw:
  - source/cli/core/execute.go
  - source/cli/core/app.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/spec/env.go
  - source/cli/core/execute_test.go
---

Checked for issue 27 (signal handling). No store note matched; grep of docs/, CONTEXT.md, agentic/ and all .go/go.mod files for signal/SIGINT/goleak/tumika found nothing relevant (CONTEXT.md:166 only says a daemon Runner's start blocks until its context is cancelled).

- `executeArgs` passes `context.Background()` to `cobra.Run` (execute.go:98); the doc comment promises it (execute.go:69-72). Both `ExecuteArgs` (execute.go:87) and `App.ExecuteArgs` (app.go:114) funnel into `executeArgs`, so one change covers both.
- Exit codes are the consts ExitOK/ExitError/ExitUsage = 0/1/2 (execute.go:14-24). Mapping is `errors.As(*cobra.UsageError)` -> 2, else 1 (execute.go:103-107). A context.Canceled error is not special-cased: it prints "<name>: context canceled" and exits 1. No 130 constant exists.
- os.Exit is never called in library code. It appears only in doc examples (app.go:17, doc.go:12, execute.go:31) and in test TestMain (execute_test.go:431-445). `Execute`/`App.Execute` return an int for main to pass to os.Exit (execute.go:36, app.go:87). A second-signal "exit immediately with 130" therefore cannot call os.Exit from ExecuteArgs without breaking this convention.
- ExecuteArgs "touches no part of the process that env does not give it" (execute.go:69). A process-wide signal.Notify inside ExecuteArgs breaks that contract. `spec.Env` (source/cli/spec/env.go:10-23) has only Stdin, Stdout, Stderr, WorkDir, LookupEnv, with nil-field defaults in `Resolved()` (env.go:28-47). An injectable signal channel would fit there or on cobra.Config.
- Concurrency is a documented guarantee: one tree runs any number of times concurrently (execute.go:86). Test TestExecuteArgsConcurrent runs 8 parallel calls (execute_test.go:406); App tests do likewise (app_test.go:241). Per-call signal registration must not share state. Tests do not use t.Parallel (grep found none).
- Hooks (PreRun/PostRun) and run functions all receive ctx via cobra.Config / cmd.ExecuteContext (cobra/cobra.go:59,64,110).
- Dependencies of cli/core: cobra, pflag (go.mod). No goleak present. Adding a third-party test dep would be new.
- Real-process tests exist: TestMain re-execs the test binary as a child (execute_test.go:425-445), a ready pattern for a real-SIGINT-to-child test of the 130 path.

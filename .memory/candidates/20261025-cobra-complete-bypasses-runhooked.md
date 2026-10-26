---
about: "cobra's hidden __complete is live in the cli/core adapter and bypasses every hook; completion is an unknown command"
saw:
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/app.go
  - source/cli/core/execute_test.go
  - source/cli/spec/command.go
---

Verified 2026-10-25 with a throwaway probe test in internal/cobra (deleted afterwards), cobra v1.10.2.

- `cobra.go:100` sets `DisableDefaultCmd = true` (drops only `completion`); cobra still adds hidden
  `__complete`/`__completeNoDesc` in ExecuteC (cobra command.go:1110, completions.go:231). Its Run is cobra's
  own, so `runHooked` (`cobra.go:196`) is never reached: PreRun, PostRun, config hook, logging hook, update
  check, Output resolver all skipped. Probe: `[__complete gr]` -> err nil, prerun=0,
  stdout `greet\tg\n:4\n`, stderr `Completion ended with directive: ShellCompDirectiveNoFileComp\n`.
- Exit 0, not unknown command. `completion` -> UsageError "unknown command" (exit 2), pinned by
  `execute_test.go:215-219`; `notWant "completion"` in help at `execute_test.go:260`, `app_test.go:103`.
- Quirk: `[__complete ""]` prints a blank first line, from the hidden `SetHelpCommand(&spf.Command{Hidden:true})`
  stub (`cobra.go:101`), which cobra lists as completion candidate.
- All hooks reach cobra only via `Config.PreRun/PostRun` (`app.go:217`); `logFlags.flush` runs in
  `executeArgsEnded` (`app.go:218`), outside cobra.
- spec.Command.Validate (`spec/command.go:79`, Args at `:125`) has no completion field; a new one would be
  validated in `validate()` beside Args. No custom help renderer: cobra default templates; goldens in
  `testdata/render` are error renders only. spec has no command groups.
- ADR numbers: highest is 0019; duplicates at 0009,0010,0012,0014,0015,0016,0018,0019; next free 0020.

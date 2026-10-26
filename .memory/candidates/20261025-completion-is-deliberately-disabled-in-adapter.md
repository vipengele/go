---
about: "cobra's default completion and help subcommands are deliberately disabled in cli/core, and tests assert their absence; flags have no enum or path kind"
saw:
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/execute.go
  - source/cli/core/execute_test.go
  - source/cli/core/app_test.go
  - source/cli/spec/flag.go
---

Checked while planning issue #43 (shell completion).

- `internal/cobra/cobra.go:100-101`: `cmd.CompletionOptions.DisableDefaultCmd = true` and `cmd.SetHelpCommand(&spf.Command{Hidden: true})`. Doc comments at `cobra.go:91` and `execute.go:142-143` state "no help or completion subcommand".
- Tests assert it: `execute_test.go:211-219` ("help is not a subcommand", "completion is not a subcommand" -> exit 2, unknown command) and `notWant: "completion"` at `execute_test.go:260`, `app_test.go:103`. Wiring completion must change these.
- `spec.Command.Hidden` exists (`spec/command.go:31`), passed through at `cobra.go:124`.
- Flags: only BoolKind/StringKind/IntKind (`spec/flag.go:12-20`); no enum/choices, no path kind, no ValidArgs on spec.Command (grep for ValidArgs/enum/MarkFlagFilename: 0 hits in source/cli). Enum-ish flags (--output text|json, --log-format) are validated in cli/core by hand.
- ADR-0003 lists "shell completion" as cli scope (lines 19, 78); no ADR decides on it.
- Only bash exists on the dev box (`which zsh fish pwsh` empty); runner images unverified. ci-test.yml uses ubuntu-latest.
- cli/core has no tags and no docs/release-notes/cli dir -> not strict mode.

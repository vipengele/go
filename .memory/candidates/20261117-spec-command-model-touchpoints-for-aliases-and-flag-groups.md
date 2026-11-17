---
about: "where aliases and flag groups would have to be threaded: spec.Command + Validate, the single cobra adapter build(), UsageError wording; no alias or flag-group support exists today"
saw:
  - source/cli/spec/command.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/exitcodes.go
  - source/cli/core/config.go
  - docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md
  - docs/adr/0020-completion-is-an-opt-in-app-option-over-a-neutral-spec-model.md
---
Checked 2026-11-17. `grep -rniE alias --include=*.go source/cli source/update source/daemon` (non-test) -> 0 hits; no abandoned attempt in docs/adr.

- Parser is cobra, wrapped by ADR-0005: model in cli/spec (no cobra import, depguard), adapter in cli/core/internal/cobra only. ADR-0005 "Considered options" rejects exposing *cobra.Command (cobra would leak into every project's API); "The model can express only what the adapter maps" - a feature must be added to spec.Command and mapped in the adapter.
- Only one translator: cobra.go build() (cobra.go:~136-210) sets cobra.Command{Use: c.Name, Short, Long, Hidden}. Aliases = set `Aliases` there; cobra then handles dispatch, help listing and __complete for aliases. Other tree walkers: spec/command.go validate (subcommand duplicates checked by Name only, command.go ~line 185 `seen[sub.Name]`), core/config.go:153 (walks cmd.Commands for failRuns), core/command/config/config.go:147; update/daemon only build commands. completioninstall and speccheck do not walk Commands by name (grep `\.Commands` non-test).
- Validate collects errors as *CommandError joined (command.go:70-90); collision rule belongs beside the `seen` map; reserved cobra names (`help` flag reserved at cobra.go:~146; hidden help command; `completion` when WithCompletion) are not checked by Validate today.
- Usage errors: adapter type cobra.UsageError{Path, Err} (cobra.go:26-45) returned from flag-error func (cobra.go:112-114, so cobra's own wording e.g. "unknown flag: --x"), Args check (cobra.go:~208), Output err (cobra.go ~219), unknownCommand (`unknown command %q for %q`, cobra.go:~310). Rendered `<Path>: <Err>`. Exit 2 = ExitUsage (core/execute.go:29), chosen at execute.go:198 rule 4 for *cobra.UsageError or a *spec.UsageError in the chain (ADR-0015). Registry codes cannot be 2 (exitcodes.go:19-22). A group violation detected in the adapter before run should return cobra.UsageError; if checked in spec it needs a spec.UsageError or the adapter wrap.
- Flags: spec.Flag has Inherited() (spec/flag.go:50,93,188) -> adapter puts it in cmd.PersistentFlags() (cobra.go:155-156) and passes `children` down so descendants read it via `visible`; read() (cobra.go:348-373) builds values and the `set` list from fs.Changed. Changed-ness is available for inherited and local flags alike through the one FlagSet, so a group over a parent's persistent flag + a local flag is feasible in the adapter; Validate's flagScope (command.go ~100-115) already resolves inherited names so group membership could be validated there.

---
about: "cli output format is spec.Output.Format() resolved in core/output.go from --json or --output (never both on one App); JSON is written by spec.WriteJSON; --json=false vs unset is only knowable through spec.ExplicitFlags fed by the cobra adapter"
saw:
  - source/cli/spec/output.go
  - source/cli/spec/json.go
  - source/cli/spec/invocation.go
  - source/cli/spec/flag.go
  - source/cli/spec/speccheck/speccheck.go
  - source/cli/core/flags.go
  - source/cli/core/output.go
  - source/cli/core/app.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/command/version/version.go
  - docs/adr/0014-json-output-envelope.md
---

Facts that break silently or panic if changed:

- The format lives in `spec.Output` (`Format()`; `JSON()` is derived), in `cli/spec` so update/daemon commands
  need no cli/core (ADR-0009). `core/output.go` `outputResolver.format` builds it from the standard `--json`
  (`WithJSON`) or the standard `--output text|json` (`WithOutput`). `app.go` `New` panics when an App takes both,
  because `Command.Validate` rejects a descendant redeclaring an inherited flag name (`source/cli/spec/command.go`).
- A command reads `inv.Output.Format()`, never a flag handle. `version` is the exception that keeps a local
  `--json` (the `update` preflight runs `<staged> version --json`, ADR-0007) and ORs it with the format, so
  `version --json` and `version --output json` both print JSON, with or without `WithOutput`.
- The cobra adapter stores every visible flag's value, default included, so `--json=false` and an absent `--json`
  read identically through `FlagOf.Get`. The usage-error rules (`--output json` with an explicit `--json=false`,
  `--output text` with a true `--json`) need `FlagOf.IsSet`, which is true only for names the adapter passed in
  `spec.ExplicitFlags`, filled from pflag's `Changed` in `internal/cobra/cobra.go` `read`. A new adapter that does
  not pass them makes every flag read as unset.
- JSON data is written by `spec.WriteJSON(inv.Env.Stdout, schema, v)` (ADR-0013: data on stdout): indented, `schema`
  first, error for a non-object, `schema < 1`, or a value that already has `schema`. `buildinfo.Info.MarshalJSON`
  emits no `schema`; `command/version` supplies 1.
- `speccheck.AssertJSONKeys` rewrites its golden file from the env var `SPECCHECK_UPDATE=1`, not a flag:
  `gochecknoglobals` is on in `cli/spec`, and `cli/core` already registers `-update` in its `TestMain`
  (`source/cli/core/execute_test.go`), so a second `-update` in one test binary panics.
- Depguard: cobra only in `core/internal/cobra`; x/term only in `spec/terminal`. No testify/go-cmp in cli go.mod files.

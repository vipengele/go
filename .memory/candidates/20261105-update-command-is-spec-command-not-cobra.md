---
about: "update's command is updatecmd.Commands(cfg) []*spec.Command, not cobra (ADR-0005); a tool joins it with app.Add, since cli/core.App has no AddCommand; Apply is the one-call update and hooks stay with the caller"
saw:
  - docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md
  - docs/adr/0009-the-app-owns-version-and-takes-hooks-as-options.md
  - source/cli/core/app.go
  - source/update/.golangci.yml
  - source/update/apply.go
  - source/update/updatecmd/updatecmd.go
  - source/update/example_test.go
---

- The `update` command is `updatecmd.Commands(cfg update.Config, opts ...Option) []*spec.Command`
  (source/update/updatecmd/updatecmd.go), framework-neutral per ADR-0005:47-68,90. ADR-0003's
  cobra-in-updatecmd allowance is superseded: source/update/.golangci.yml's `no-cobra` rule denies
  `github.com/spf13/cobra` for files `**`, beside `core-stays-out` for `cli/core/**` (tests included).
- ADR-0005:68 and ADR-0003:42 write the join as `app.AddCommand(...)`, but `*cli/core.App` has only
  `Add(cmd *spec.Command)` (source/cli/core/app.go:173), one command per call; ADR-0009:29,85 records
  the correct name. A tool's main loops: `for _, c := range updatecmd.Commands(cfg) { app.Add(c) }`.
- `Config.Apply` (source/update/apply.go) is the orchestrator: CheckManaged -> CheckWritable ->
  Download -> Target -> Extract into the target's dir -> Install, returning `*Installed` with the
  backup kept. Post-update hooks and Discard/Rollback stay with the caller; a hook failure is
  report-only (hook.go doc, ADR-0021), and source/update/example_test.go shows it with no Rollback.
- `--check` exits `updatecmd.ExitUpdateAvailable` (10) through `spec.Coded(10, spec.Reported(err))`
  (cli/spec/exit.go). Confirmation uses `inv.Confirm` with `Required: true`, so a non-terminal stdin
  is cli/spec's `*spec.UsageError` (exit 2) rather than a silent "no" (cli/spec/prompt.go:81-88).

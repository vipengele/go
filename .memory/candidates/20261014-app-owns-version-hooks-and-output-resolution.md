---
about: "cli.App owns the version command and takes hooks and standard flags as options; hook and output behaviours live in the cobra adapter's RunE, and cli/core never imports update"
saw:
  - source/cli/core/app.go
  - source/cli/core/hooks.go
  - source/cli/core/output.go
  - source/cli/core/execute.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/command/version/version.go
  - source/cli/spec/command.go
  - docs/adr/0009-the-app-owns-version-and-takes-hooks-as-options.md
---

Facts that break silently if changed:

- `App.root` appends the `version` command to every execution's root (`source/cli/core/app.go`), so a tool
  that also adds its own `version` gets a duplicate name and "invalid command tree" (exit 1). `version` has a
  local `--json` unless the App took the standard `--json` (`WithJSON`); then it declares no flag and reads
  `inv.Output.JSON()` (`version.WithOutputJSON`), because `Command.Validate` rejects a descendant flag that
  collides with an inherited one (`source/cli/spec/command.go`). Under `WithOutput` the local flag stays and is
  ORed with the resolved format. Dropping the local flag outright would make
  `version --json` exit 2 for tools without `WithJSON` and fail `update`'s `<staged> version --json` preflight.
- `version.New` carries the `spec.SkipUpdateCheck` annotation so an update-check hook skips the preflight run.
  An annotation applies to the command carrying it only, never its subcommands. `cli/core` never imports `update`
  and nothing in production reads the annotation yet: the tool's `main` wires the check as a hook.
- Hooks are `App` options, not `spec.Command` fields, and reach the adapter as plain function slices in
  `cobra.Config` (`source/cli/core/internal/cobra/cobra.go`). Order inside `RunE`: flags parsed, `Config.Output`
  resolved (a `-v` with `-q` error is a `UsageError`, exit 2, and no hook fires), pre-run hooks in order, `Run`,
  post-run hooks in reverse. A post-run error is returned as `cobra.HookError`, which has no `Unwrap`, so
  `errors.As(*UsageError)` in `execute.go` never finds a usage error through it and a hook can never cause exit 2.
- Output resolution and hooks only happen for a command that has a `Run`: `tool -v -q` on a Run-less parent just
  prints help with exit 0. The package-level `ExecuteArgs` resolves with a zero `outputResolver`: no standard flags
  and no `<PREFIX>_NO_COLOR`, but `NO_COLOR` and the terminal check still decide colour (`source/cli/core/output.go`).
- Colour is off when stdout is not an `*os.File` on a character device, so `/dev/null` counts as a terminal;
  tests use `os.DevNull` for the true case and a temp file for the false one rather than a real TTY.

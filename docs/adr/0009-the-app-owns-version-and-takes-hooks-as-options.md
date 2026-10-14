# The `App` owns `version` and takes hooks as options

Status: amends ADR-0003 and ADR-0005.

A tool built on `cli/core` assembles its commands into a `cli.App` in its `main`: `cli.New` takes
the tool's `spec.Identity` and options, `Add` adds a `spec.Command` tree, and `Execute` runs it.
The options opt into the standard flags (`--verbose`, `--quiet`, `--no-color`, `--json`) and add
the hooks that run around every run function. Every execution builds its own root from those, so
one `App` runs any number of times, concurrently.

## Hooks are options of the `App`

A pre-run hook runs before the run function of the command that executes and a post-run hook after
it. Each receives the `Invocation` and a `spec.CommandInfo`: the command's path from the root and
its annotations. Hooks are given to `cli.New` as options; `spec.Command` has no field for them.

A `spec.Command` tree is immutable data that any number of executions, and any number of tools,
share (ADR-0005). A hook is a property of the tool that runs the tree, not of the tree: an update
check belongs to the tool's `main`, and a command contributed by `update` or `daemon` should not
carry it. Kept on the `App`, hooks need no cobra and no change to the model, and the adapter in
`internal/cobra` calls them as plain functions.

A command that a hook should treat differently says so in its `Annotations`, key-value metadata
the hook reads through `CommandInfo`. An annotation applies to the command that carries it alone.

## The method is `Add`

The method that adds a command tree to the `App` is `Add`, called once per tree. ADR-0003 and
ADR-0005 write it as `app.AddCommand(...)`; the name in the code is `Add`, and a tool's `main`
joins the parts with `app.Add(...)`.

## `Output` lives in `cli/spec`

What the standard flags and the environment decide for one execution, whether it is verbose or
quiet, writes JSON and may use colour, is a `spec.Output` on the `Invocation`. The commands that
`update` and `daemon` contribute depend on `cli/spec` alone (ADR-0005, ADR-0008), and they read the
output mode the same way a tool's own commands do. A type in `cli/core` would put the app shell, and
cobra with it, under every project that contributes a command.

## The `App` owns `version`

Every root the `App` runs holds the `version` command of `cli/core/versioncmd`, after the added
commands. A tool does not add it; a command named `version` that a tool adds is a duplicate name,
and the tree is invalid. The build information is the running binary's, read with
`buildinfo.Read`, unless an option gives it.

`version --json` is the contract ADR-0007 defines, and `update`'s preflight runs
`<staged> version --json` on every staged binary. Owned by the `App`, the command is present in
every tool, so the preflight never meets a tool that forgot to add it.

Whether `--json` is the standard flag decides how `version` reads it. Without `WithJSON`, `version`
declares a local `--json` of its own. With `WithJSON`, `--json` is an inherited flag of the root,
and `spec.Command.Validate` rejects a descendant that declares a flag of the same name; `version`
then declares no flag and reads `Output.JSON()`. Either way `version --json` writes the JSON form.
The `App` builds the command for each execution, so its flag handle belongs to that execution.

`version` carries the `SkipUpdateCheck` annotation: it reports the binary that runs, and the
preflight runs it on a binary that is not yet installed.

## The update check is wired by the tool

`cli/core` never imports `update`. A tool that checks for updates adds the check as a pre-run or
post-run hook in its `main`, where it already holds the `Identity` it hands to both projects
(ADR-0008). A command annotated with `spec.SkipUpdateCheck` is exempt from the check.

## Considered options

- **Hooks as fields of `spec.Command`.** Puts a property of one tool into data that several tools
  share, so the same tree cannot run with and without the hook, and a command contributed by
  another project carries hooks its contributor never chose.
- **`Output` in `cli/core`.** Keeps it beside the resolver that builds it, but a command from
  `update` or `daemon` can then read it only by requiring `cli/core`.
- **`version` added by the tool, with the standard `--json` replacing its local flag.** Dropping
  the local `--json` from `version` outright makes `version --json` a usage error, exit 2, in every
  tool that does not opt into the standard `--json`, and the self-update preflight fails on that
  tool's staged binaries. A tool that forgets to add `version` fails the preflight the same way.
- **`cli/core` installs the update check itself.** Makes `cli/core` require `update`, so every
  tool takes self-update whether it wants it or not, and reverses the dependency direction of
  ADR-0005 and ADR-0008.

## What this changes in earlier ADRs

- **ADR-0003.** The `App` it assigns to `cli` takes the standard flags and hooks as options, and
  its version command is not optional.
- **ADR-0005.** A tool's `main` joins the parts with `app.Add(...)`, not `app.AddCommand(...)`.

## Consequences

- **A tool cannot define its own `version`.** Its build information comes from `buildinfo`, set at
  link time or read from the module's build information.
- **A command that needs no update check says so in its annotations.** A hook that skips commands
  reads the annotation; it keeps no list of command names.
- **Opting into `--json` changes no command line that worked before.** `version --json` writes JSON
  in both modes, and `--json` before `version` is accepted only once the standard flag exists.

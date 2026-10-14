# Commands are a framework-neutral model in `cli/spec`; cobra is an internal adapter in `cli/core`

Status: partially supersedes ADR-0003, which it replaces in two rules: where cobra is allowed, and
that `update` requires no sibling.

A tool built on the library is assembled from commands contributed by several projects: the app
shell's `version`, `update`'s `update`, `daemon`'s service commands, and the tool's own. If a command
is a `*cobra.Command`, cobra is part of every one of those projects' public API and of every
consumer's code, and replacing it is a change to all of them. A command is instead data that
describes itself, and cobra is one way to run it.

## The model

`cli/spec` (ADR-0004) declares the model. It imports no CLI framework.

- **Command**: a name, a short and a long description, its flags, its subcommands, and a run
  function that takes an **Invocation** and returns an error.
- **Flag**: a name, a usage string, a default and a type. A flag is created by a typed constructor
  that returns a handle, and the handle is listed in the `Flags` of the command that owns it.
- **Invocation**: what a run function receives for one execution: the parsed flag values, the
  positional arguments and the `Env`.

A flag's value is read through its handle:

```go
asJSON := spec.BoolFlag("json", "print JSON")

cmd := &spec.Command{
	Name:  "status",
	Flags: []spec.Flag{asJSON},
	Run: func(ctx context.Context, inv *spec.Invocation) error {
		if asJSON.Get(inv) {
			// ...
		}
		return nil
	},
}
```

A `Command` tree is immutable data. It holds no per-execution state, so one tree can be run any
number of times, in parallel, and by a test that substitutes the `Env`. Everything an execution
produces lives in its `Invocation`.

## The adapter

`cli/core` holds the only code that knows cobra, in `internal/cobra`. It translates a `spec.Command`
tree into a cobra tree, binds each flag handle to a cobra flag, runs it, and fills the `Invocation`
the run function reads. `github.com/spf13/cobra` appears in `cli/core`'s `go.mod` and nowhere else:
not in `cli/spec`, and not in `update`, `daemon` or a consumer. Each project's `.golangci.yml`
carries a `depguard` rule that denies cobra outside `internal/cobra`, so the boundary is checked
rather than remembered. Replacing cobra changes `internal/cobra` and no consumer code.

## Dependency direction

```
cli/core ──► cli/spec
update   ──► cli/spec
daemon   ──► cli/core, cli/spec, update
cli/spec ──► commons
```

- `cli/spec` requires `commons` and no project that holds an engine or a framework.
- `update` requires `cli/spec` so that its package of commands returns `[]*spec.Command`. It does
  not require `cli/core` and takes no cobra with it.
- `cli/core` requires `cli/spec`, and runs the commands it is given. While the two are developed
  together the `require` carries a directory `replace` (ADR-0006).
- The tool's `main` joins the parts: `app.AddCommand(updatecmd.Commands(cfg)...)`.

## Considered options

- **Expose `*cobra.Command` from `cli/core`, as `versioncmd.New` does.** Needs no model and no
  adapter, but every contributing project and every consumer imports cobra, a cobra major version
  is a breaking change to all of them, and the `depguard` rule can only name the packages where
  cobra is allowed, not stop it spreading.
- **The model in `cli/core` itself.** Keeps the model next to the adapter, but `update` can then
  contribute a command only by requiring `cli/core`, which brings the whole app shell and its cobra
  dependency to a tool that wants self-update alone. The model is in its own project so that the
  contract can be required without the thing that runs it.
- **Flag values by string key (`inv.Bool("json")`).** Needs no handle, but a misspelt key or a
  mismatched type is found when the command runs, and a flag can be read by a command that did not
  declare it. A typed handle is checked by the compiler, and the command's `Flags` list is the
  complete declaration.
- **A mutable command builder with flag values bound to variables, as cobra does.** Familiar, but
  the tree carries the state of one execution, so it cannot be reused or run twice, and the model
  ends up shaped like cobra.

## Consequences

- **ADR-0003's cobra rule is replaced.** Its per-project allowance (`cli`, `update/updatecmd`,
  `daemon/daemoncmd`) becomes one place, `cli/core/internal/cobra`. The engine packages still
  import no command framework.
- **ADR-0003's dependency direction changes.** `update` requires `cli/spec`, which is a sibling.
  The reasoning that a tool wanting only self-update takes nothing else of this repo holds for
  `cli/core` and `daemon`, not for the contract.
- **A change to `cli/spec` selects `cli/core`, `update` and `daemon` in CI**, because their `go.mod`
  files require it (ADR-0006). The model is small and changes rarely; a change to it is a change to
  the contract every command depends on.
- **The model can express only what the adapter maps.** A cobra feature with no counterpart in
  `spec.Command` is unavailable to commands until the model gains it and the adapter maps it.

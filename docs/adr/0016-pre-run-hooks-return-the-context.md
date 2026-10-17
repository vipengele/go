# Pre-run hooks return the context

Status: accepted. It amends the hook shape of
`0009-the-app-owns-version-and-takes-hooks-as-options.md`; the rest of that ADR stands.

A pre-run hook added with `cli.WithPreRun` is

```go
type PreRunHook func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error)
```

The context it returns is the one every later pre-run hook, the run function and every post-run
hook of that execution receive. A hook that has nothing to hand on returns `ctx`. A nil context
keeps the one the hook received, so a hook that returns `nil, nil` neither crashes the run nor
drops the run's cancellation. When a hook returns an error, the execution stops as ADR-0009
describes, neither the run function nor any post-run hook is called, and the context returned
with the error reaches nothing.

## Why the hook returns a context

ADR-0009 gives a pre-run hook the `Invocation` and the command that runs and lets it return only an
error, so it can inspect a run and stop it but cannot give anything to the code after it. A hook
that loads state for every command, configuration first, has to hand the result to later hooks and
to the run function. `context.Context` is already what every hook and run function receives, and
the run's cancellation travels in it
(`0012-signals-are-registered-by-execute-and-injected-through-env.md`), so a hook derives its context from the
one it receives with `context.WithValue` and the value reaches the run function with cancellation
intact. A package that stores a value keeps its key unexported and offers typed accessors, so the
contract is between that package and its readers, and `cli/core` stores nothing itself.

The post-run hook keeps its shape: nothing runs after it that could read what it returned.

## Considered options

- **A handle on `Invocation`.** A field or method of `spec.Invocation` that a hook writes to and a
  run function reads from. The invocation is `cli/spec` data that commands from `update` and
  `daemon` receive too (ADR-0009, "`Output` lives in `cli/spec`"); a general-purpose slot on it
  puts an untyped bag into the model every project shares, and `cli/spec` would have to define
  how values are keyed, overwritten and kept apart between executions.
- **Typed values on `Invocation`.** A field per kind of state, such as a configuration field.
  `cli/spec` would then name every kind of state any hook loads, and a hook added by a tool's
  `main` could not hand on a type `cli/spec` does not know.
- **Keep the error-only hook and load inside each run function.** Every command that reads the
  configuration loads it again, and a broken file fails once per command instead of once, before
  any run function, as a hook error.

## Consequences

- **Every pre-run hook returns a context.** This is a breaking change to the `PreRunHook` type.
  It is taken while `cli/core` has no release, and no project outside `cli/core` uses
  `WithPreRun`, so no consumer has code to change.
- **What a hook loads is read from the context.** A run function or a later hook reads it through
  the accessors of the package that stored it, and finds nothing when that hook did not run or
  skipped the command.
- **The order of `WithPreRun` options is the order of the data.** A hook sees what the hooks added
  before it stored, and nothing a later hook stores.

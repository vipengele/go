# cli/core

Module `go.vipengele.org/cli/core`, tag `source/cli/core/vX.Y.Z`. The base of the vipengele CLI
library. The `version --json` output is a contract owned by the `cli` project (ADR-0007).

## Packages

- `cli` (the root package) — `New(identity, opts...)` builds an `App`; `Add(cmd)` adds a command
  tree; `Execute()` / `ExecuteArgs(env, args)` run it, each execution building a fresh root named by
  `identity.Name`. Options: `WithVerbose`, `WithQuiet`, `WithNoColor`, `WithJSON` opt into the
  standard flags, declared as inherited flags on the root (`flags.go`); `WithPreRun` / `WithPostRun`
  add `PreRunHook` / `PostRunHook`s (`hooks.go`), which receive the `Invocation` and a
  `spec.CommandInfo` (path and `Annotations`); `WithBuildInfo` fixes what `version` prints (tests),
  otherwise `buildinfo.Read()`. A run function reads the standard flags and colour from
  `inv.Output` (`spec.Output`, resolved in `output.go`), never from a handle. The App owns `version`
  (ADR-0009): every root holds `versioncmd.New(...)` after the added commands, with a local `--json`
  unless `WithJSON` is given, then `versioncmd.WithOutputJSON()`; a tool's own `version` is a
  duplicate name. `cli/core` never imports `update`: the tool's `main` wires the update check as a
  hook, and `spec.SkipUpdateCheck` is the opt-out annotation.
  The package-level `Execute(root)` runs a `*spec.Command` tree for a tool's `main` against
  the process (`os.Args[1:]`, standard streams, working directory, environment) and returns the exit
  code; `ExecuteArgs(env, root, args)` is the same against a given `*spec.Env`, for tests. Both
  validate the tree on every call. Exit codes: 0 success and `--help`, 1 a run error or an invalid
  tree, 2 a command line the tree does not accept, 130 interrupted (root context cancelled and
  `context.Canceled`, nothing printed); 3-125 are registrable. Order, first match wins: 130,
  `spec.Coded` (1 or 3-125), `WithExitCode(code, sentinel)` / `WithExitCodeAs(fn)` in option order,
  usage 2, else 1. The registry is per-`App`, read-only after `New` (a bad code panics there); the
  package-level `Execute` / `ExecuteArgs` have none. Every error is written to stderr as
  `<root name>: <error>` unless it carries `spec.Reported` (the code is still chosen as above; the
  `invalid command tree` line is never suppressed). A post-run hook error after a successful run is
  an opaque `*HookError` (exit 1, printed); a pre-run hook error maps like a run error (ADR-0010).
- `internal/cobra` — the only code that imports cobra. `Run(ctx, env, root, args, Config)` builds a
  fresh cobra tree per execution from a validated `*spec.Command`, so one tree may run any number of
  times, concurrently. `Config` carries the per-run `Output` resolver and the `PreRun` / `PostRun`
  hooks as plain funcs; a post-run error after a successful run is a `*HookError`, never a
  `*UsageError`.
- `buildinfo` — `Read()` returns an `Info` (version, commit, date, Go version, OS, arch). Standard
  library only. `Info.String()` is the one-line text form; `Info.MarshalJSON()` is the
  schema-versioned JSON form (`schema` is 1). Adding a key keeps `schema`; renaming, removing or
  retyping a key increments it.
- `versioncmd` — `New(info, opts...)` returns the `*spec.Command` for `version`, annotated
  `spec.SkipUpdateCheck`. By default it has a local (not inherited) `--json` flag;
  `WithOutputJSON()` declares none and reads `inv.Output.JSON()`, for a root with an inherited
  `--json` (a descendant flag of the same name makes the tree invalid).

## Gotchas

- Link-time values: `buildinfo.version`, `commit` and `date` are set with `-X` and must stay
  uninitialised package-level `string` variables that `Read` references. The linker ignores any
  other `-X` silently. `buildinfo/ldflags_test.go` builds a real binary
  (`buildinfo/testdata/ldflagsmain`) to prove it, so that test needs the `go` command.
- Cobra lives only in `internal/cobra`, imported under the alias `spf`; the depguard rule in
  `.golangci.yml` fails any import of `github.com/spf13/cobra` elsewhere. `cli/spec` is cobra-free.
- Commands are `cli/spec` data. Flags are read through typed handles (`asJSON.Get(inv)`) built inside
  the command's constructor: `gochecknoglobals` is on, so there are no package-level handles, and a
  package-level variable needs a justified `//nolint`.
- Sibling edges: `go.mod` requires `go.vipengele.org/cli/spec` and `go.vipengele.org/commons`
  (indirect, because spec imports it), each with a version-less directory `replace` (`../spec`,
  `../../commons`) while they are developed together (ADR-0006). The main module ignores a
  dependency's `replace`, which is why commons is listed here.
- An `ExecuteArgs` test passes a fake `Env` (buffers, a fake `LookupEnv`, a `t.TempDir` `WorkDir`).
  A real environment variable set with `t.Setenv` is invisible to `Run`, which reads only
  `Env.LookupEnv`.

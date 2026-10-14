# cli/core

Module `go.vipengele.org/cli/core`, tag `source/cli/core/vX.Y.Z`. The base of the vipengele CLI
library. The `version --json` output is a contract owned by the `cli` project (ADR-0007).

## Packages

- `cli` (the root package) — `Execute(root)` runs a `*spec.Command` tree for a tool's `main` against
  the process (`os.Args[1:]`, standard streams, working directory, environment) and returns the exit
  code; `ExecuteArgs(env, root, args)` is the same against a given `*spec.Env`, for tests. Both
  validate the tree on every call. Exit codes: 0 success and `--help`, 1 a run error or an invalid
  tree, 2 a command line the tree does not accept. Every error is written to stderr as
  `<root name>: <error>`.
- `internal/cobra` — the only code that imports cobra. Builds a fresh cobra tree per execution from a
  validated `*spec.Command`, so one tree may run any number of times, concurrently.
- `buildinfo` — `Read()` returns an `Info` (version, commit, date, Go version, OS, arch). Standard
  library only. `Info.String()` is the one-line text form; `Info.MarshalJSON()` is the
  schema-versioned JSON form (`schema` is 1). Adding a key keeps `schema`; renaming, removing or
  retyping a key increments it.
- `versioncmd` — `New(info)` returns the `*spec.Command` for `version`, with a local (not
  persistent) `--json` flag.

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

# cli/core

Module `go.vipengele.org/cli/core`, tag `source/cli/core/vX.Y.Z`. The base of the vipengele CLI
library. The `version --json` output is a contract owned by the `cli` project (ADR-0007).

## Packages

- `buildinfo` — `Read()` returns an `Info` (version, commit, date, Go version, OS, arch). Standard
  library only. `Info.String()` is the one-line text form; `Info.MarshalJSON()` is the
  schema-versioned JSON form (`schema` is 1). Adding a key keeps `schema`; renaming, removing or
  retyping a key increments it.
- `versioncmd` — `New(info)` returns the cobra `version` command, with a local (not persistent)
  `--json` flag.

## Gotchas

- Link-time values: `buildinfo.version`, `commit` and `date` are set with `-X` and must stay
  uninitialised package-level `string` variables that `Read` references. The linker ignores any
  other `-X` silently. `buildinfo/ldflags_test.go` builds a real binary
  (`buildinfo/testdata/ldflagsmain`) to prove it, so that test needs the `go` command.
- Cobra stays inside `versioncmd`: `.golangci.yml` has a depguard rule that fails any import of
  `github.com/spf13/cobra` elsewhere. `gochecknoglobals` is on, so a package-level variable needs a
  justified `//nolint`.

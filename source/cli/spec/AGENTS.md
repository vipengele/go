# cli/spec

`go.vipengele.org/cli/spec`: the framework-neutral model of a command-line tool (`Identity`, `Command`,
`Flag`, `Invocation`, `Env`) that `cli/core`, `update` and `daemon` share. Read `doc.go` first, then
`docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md`
and `docs/adr/0009-platform-paths-live-in-cli-spec-paths.md` at the repo root.

## Commands (run from `source/cli/spec`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Structure

- Root package `spec` — performs no I/O: it imports no `os` and touches neither the environment nor the
  file system. Anything that does belongs in a subpackage.
- `Coded(code, err)` / `Reported(err)` (`exit.go`) — the exit code and the already-shown mark a run
  function returns; they live here so `update` and `daemon` need not import `cli/core`, which maps
  them to an exit code (ADR-0010).
- `paths/` — `paths.Resolve(spec.Identity, paths.Options, kinds...)` resolves the requested config,
  state, cache, data and log directories per OS layout (XDG, darwin, windows), honouring
  `<PREFIX>_HOME`, `Options.Home` and explicit per-kind options, and `paths.New` resolves all five.
  A kind not requested stays "" and nothing is looked up for it, so a caller that needs only state
  does not fail on a home directory only the other kinds need; a requested kind still resolves the
  kind it derives from (log from state on XDG, state and data from config on darwin). Resolving
  creates and stats nothing; `Dirs.Ensure` creates directories 0700 on demand and reports a kind
  that was not resolved. `InContainer` is a heuristic over injectable `Probes` and changes no path.

## Conventions

- Environment and home directory reach `paths` only through `Options` seams (`LookupEnv`,
  `UserHomeDir`, `GOOS`), never `os.Getenv` directly in the resolving code, so tests substitute them.
  The layout is selected by `Options.GOOS`, not by build tags.
- No package-level mutable state: `gochecknoglobals` is enabled.
- `depguard` forbids `github.com/spf13/cobra` here; the cobra adapter lives in `cli/core/internal/cobra`.
- `commons` is a sibling reached by a directory `replace` in `go.mod` (`../../commons`); see the
  repo-root rule on sibling replaces.

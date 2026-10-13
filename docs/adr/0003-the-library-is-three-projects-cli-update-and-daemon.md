# The library is three projects: `cli`, `update` and `daemon`

Status: amended by ADR-0004, which replaces the project inventory and narrows the rejection of a
shared project, and by ADR-0006, which replaces how `daemon` requires its siblings and the order a
feature spanning two projects is released in.

The library serves two shapes of tool: one-shot command-line tools and long-running daemons.
Self-update serves both. Each project is a module released alone (ADR-0001), so the split decides
which dependencies a consumer inherits. The library is three projects:

| Project | Owns |
|---------|------|
| `cli` | The app shell: `App`, root command, output (text and `--json`), exit codes, config loading, platform paths, a state store with a file lock, logging, shell completion, the version command. |
| `update` | Self-update: release source, version comparison, asset selection, download and verification, archive extraction, atomic replacement of the managed binary, check throttling and its state, the notice and the `update` command, install script and release templates. |
| `daemon` | The long-running runtime: runners and supervisor, lifecycle, health, the HTTP server kit, service managers (systemd, launchd, Windows) that register the daemon as a service, the single-instance guard, and the daemon's own update flow (pending, confirm, rollback, scheduled runner). |

## Dependency direction

```
daemon ──► cli
daemon ──► update
cli, update ──► (no sibling)
```

- `cli` requires no sibling.
- `update` requires no sibling. A tool that wants only self-update takes nothing else of this repo.
- `daemon` requires `cli` and `update` (ADR-0006).
- No project requires `daemon`. A cycle between projects is not allowed; if two projects need the
  same code, it moves to the one that is lower in the graph.

A tool that is not a daemon requires `cli` and `update` and joins them in its own `main`:
`app.AddCommand(updatecmd.New(cfg))`. Neither project knows the other exists.

## How `update` takes the app identity and state location without importing `cli`

`update` is configured with plain values and small interfaces that it declares itself. `cli` has no
type that `update` names.

```go
// package update
type Config struct {
	Name      string // binary name, used in asset names and messages
	EnvPrefix string // prefix of the opt-out variable, <PREFIX>_NO_UPDATE_CHECK
	Version   string // the running version; "dev" for a local build
	Source    Source // the release source
	StateDir  string // directory for update state; empty selects the platform default
	Token     func(ctx context.Context) (string, error) // optional API credential
}
```

The tool builds the `App` from `cli` and the `Config` for `update` from the same few values, and
passes `app`'s resolved state directory as `StateDir`. The identity is declared once in the tool's
`main`, not discovered across a project boundary.

Consequences of this choice:

- `update` keeps a small platform-directory resolver for the default `StateDir`, a few lines that
  overlap with `cli`'s paths. A consumer using both passes `cli`'s directory and never reaches the
  default.
- `update` writes its state with its own atomic writer. It does not use `cli`'s store, so the two
  stores do not share a lock or a file format.
- The staged-binary preflight runs `<staged> version --json`. The JSON shape it reads is defined and
  documented by `update`; `cli`'s version command is written to emit it. The two projects agree
  through that documented contract, not through a shared type.

## The CLI framework dependency

The module's CLI framework is `github.com/spf13/cobra` (v1). It is allowed in exactly one place per
project:

- `cli`: the root command, command wiring, completion and the version command.
- `update`: the package holding the `update` command constructor (`update/updatecmd`).
- `daemon`: the package holding the service commands (`daemon/daemoncmd`).

The engine packages (`update` itself, `daemon`'s runtime, supervisor, health and service managers)
do not import cobra. A scheduled runner in a daemon runs the update engine with no command in the
process, and a consumer that embeds the engine in its own framework takes no command tree with it.
Each project's `.golangci.yml` carries a `depguard` rule that denies cobra outside its command
package, so the boundary is checked rather than remembered.

## Considered options

- **One project holding everything.** Simplest to tag, but a one-shot tool inherits the daemon's
  service-manager and server dependencies, and `daemon` cannot reach `v1` independently of `cli`.
  It repeats the rejected option of ADR-0001's single-module layout.
- **`update` inside `cli`.** A tool that wants only self-update would take the whole app shell. The
  update engine would also be free to reach into `cli` internals such as its store and paths, which
  the separate module forbids, and `daemon` could not use the engine without the shell's command
  tree.
- **`daemon` inside `cli`.** Every one-shot tool would inherit `golang.org/x/sys/windows/svc`, the
  service drivers and the HTTP server kit. Daemon releases would also version-bump every CLI
  consumer.
- **`update` requires `cli`, taking identity and state from `App`.** Removes the duplicated
  platform-directory code, at the cost of tying a tool that wants only self-update to the whole
  shell and to `cli`'s release cadence.
- **A fourth project for the shared identity and paths.** Removes the duplication without coupling
  `update` to `cli`, but adds a module to release for a handful of lines, and the identity is two
  strings.

## Consequences

- A change to `cli` or `update` selects `daemon` in CI, because `daemon`'s `go.mod` requires them
  (ADR-0006).
- A feature that spans `update` and `daemon` is one change. Their releases are ordered: `update`
  first, then `daemon` with its `require` raised to `update`'s tag (ADR-0006).
- Each project has a `go.mod`, a `.lydite` component, a `.gt-repo.yaml` `gomod` entry and a
  `go.work` line (see "CI" in `CLAUDE.md`).

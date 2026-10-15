# Platform directories are resolved by `cli/spec/paths`

Status: amends ADR-0003, ADR-0004 and ADR-0005. It reverses the clause of ADR-0003 that has
`update` keep its own small platform-directory resolver.

The directory a tool keeps its configuration, state, cache, data and logs in is resolved by the
public package `go.vipengele.org/cli/spec/paths`, in the `cli/spec` module. `update` takes the
default of `Config.StateDir` from it. `cli/core` requires `cli/spec` too (ADR-0005) and can resolve
the directories it hands to a tool with the same package, so there is one resolver for the two to
share.

`update` requires `cli/spec` and may not require `cli/core` (ADR-0005). A resolver that lives in
`cli/core` is therefore out of its reach, and a resolver in `update` is a second copy. `cli/spec` is
the one project both already require, and it already holds the `Identity` the resolver is
parameterised by (ADR-0008).

## What `cli/spec` may hold

ADR-0004 describes `cli/spec` as the contracts a CLI tool and the libraries it assembles agree on,
and ADR-0005 has its command model import no CLI framework. That stays true, and the I/O boundary is
drawn inside the module:

- The root package `spec` performs no I/O. A test holds its non-test files to importing no `os`, and
  all its files to importing no cobra.
- The `paths` subpackage touches the file system: it reads the environment and the user's home
  directory, and creates directories. It imports no cobra.

## The design

- Pure Go, no build tags and no dependency beyond the standard library and `cli/spec`.
- The operating system, the environment, the home directory and the file-system probes
  (exists, read) are seams, injected through plain `Options` and `Probes` structs. A test selects
  any layout on any host by setting `Options.GOOS`.
- Each kind takes the first that applies: its explicit per-kind option, `Options.Home`,
  `<PREFIX>_HOME`, on the XDG layout the kind's `XDG_*_HOME` when it is an absolute path (log has
  none), then the layout's default. `Home` and `<PREFIX>_HOME` are single roots under which every
  kind without its own option is a subdirectory.
- The layouts are the XDG layout for Linux and every system other than macOS and Windows, the
  Library layout for macOS, and the AppData layout for Windows.
- `Resolve` resolves only the kinds a caller asks for, plus the kind a requested kind derives from
  (log from state on the XDG layout, state and data from config on macOS), and `New` resolves all
  five. A kind not asked for is left empty and nothing is looked up for it, so its failure cannot
  fail the caller: a tool that needs only state resolves it from an absolute `XDG_STATE_HOME` with
  no home directory, although config, cache and data would fall back to it.
- `Ensure` is lazy: resolving creates nothing, and a directory is created when a caller asks for it.
  It creates missing directories with mode 0700, clears the bits beyond 0700 of a pre-existing
  directory and keeps a narrower mode, and never changes a parent that already exists. It skips the
  mode change on the Windows layout, where mode bits do not describe access.
- Container detection is a heuristic that reports and changes no path.
- `update`'s default `StateDir` is the state directory, not the configuration directory: state is
  what a tool records between runs and a user does not edit, and configuration is what a user edits.
  `update` is unreleased, so the default moves without a migration. `update` resolves the state
  kind alone.

## Considered options

- **`cli/core/paths`.** `update` may not require `cli/core` (ADR-0005), so it could not reach the
  resolver and would keep a copy of it.
- **`commons`.** `commons` holds generic building blocks with no knowledge of CLIs, updates or
  daemons. A resolver parameterised by a tool's `Identity` and encoding where a tool's files live
  is tool identity and file-system layout, which `commons` is kept free of (ADR-0004).
- **A new project.** A module to tag and release for one small package. `cli/spec` already is the
  shared home of `Identity`, which the resolver takes.
- **Build-tagged per-OS files.** Each layout compiles only on its own system, so the tests could not
  cover the macOS and Windows layouts on the Linux-only CI. Selecting the layout at run time from
  `Options.GOOS` tests all three everywhere.
- **A Windows and macOS CI matrix.** Cost and CI time for every project, to cover what the injected
  `Options.GOOS` already covers.
- **The `adrg/xdg` dependency.** A third-party module in every consumer's graph for a few path
  rules. The resolver needs the app-prefixed home variable, per-kind overrides and injected seams, so
  they would have to be wrapped around it anyway.
- **Eager creation at resolve time.** Resolving would create up to five directories in every
  process, including a tool that never writes, and a failure to create one would fail a command that
  needs none. Creation belongs to the caller that writes.
- **Resolving every kind on every call.** A caller that needs one kind would fail on a home
  directory only the others need: `update`, which needs only state, would refuse to start where
  `XDG_STATE_HOME` is set and no home directory can be found.
- **Tightening the mode of existing ancestors.** An ancestor such as `~/.config` belongs to the user
  and to other tools, and narrowing it breaks them. Only the directory a tool owns is narrowed.

## Consequences

- `update` and `cli/core` require `cli/spec`, so a change to it, `paths` included, selects both in
  CI (ADR-0006).
- `cli/spec` imports the standard library's `os` in a subpackage. A consumer that requires `cli/spec`
  only for the model imports the root package and takes no file-system code with it.
- ADR-0003 had `update` keep a small platform-directory resolver that overlaps with `cli`'s paths.
  `update` keeps none: it calls `paths.Resolve` for the state kind, and there is one resolver for
  `cli/core` to share.

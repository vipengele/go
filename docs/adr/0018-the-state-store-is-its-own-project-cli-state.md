# The state store is its own project, `cli/state`

Status: accepted. It refines the "state store with a file lock" row of the project table in
`0003-the-library-is-three-projects-cli-update-and-daemon.md`: the store is a project of its own,
and the lock is tracked in issue #39.

A tool keeps small files of its own beside its configuration: the last update check, a pending
update, a runner's record. A reader must see the old content or the new, never a partial write, and
a file that is missing, unreadable or corrupt must be told apart so the caller can choose between a
default and a failure. `go.vipengele.org/cli/state` provides that, with the module path of
`source/cli/state` (ADR-0004).

## Decisions

- **Home.** `source/cli/state`, a project under the `cli` grouping directory with its own `go.mod`,
  lydite component, `gomod` entry and `go.work` line. It requires no sibling.
- **Atomic writes.** `WriteFile(path, data, mode)` writes a temporary file in the destination's
  directory, chmods it, syncs it, renames it over the destination and syncs the directory.
  `Replace(tmp, final, mode)` does the last four steps for a file the caller already wrote, which is
  how `config edit` in `cli/core` replaces the configuration file after the editor has saved a copy.
  The temporary file is removed on every failure the package reports.
- **Typed helpers.** `LoadJSON[T]` and `LoadYAML[T]` return the value, a `LoadStatus` (`Missing`,
  `Loaded`, `Corrupt`, `Unreadable`) and an error; `SaveJSON[T]` writes at 0600. `Missing` comes
  with a nil error and the zero value, so a first run needs no special case. `Corrupt` errors match
  `ErrCorrupt` with `errors.Is`; `Unreadable` errors wrap the filesystem's error and do not.
- **Contract.** The package takes paths and nothing more: it resolves no directory (`cli/spec/paths`
  does, ADR-0009), creates no directory and takes no lock. Writing into a directory that does not
  exist is an error.
- **Who depends on it.** `cli/core` requires `cli/state`. Whether `update` does is left to issue
  #54; this ADR does not change ADR-0005's limit of `update` to `cli/spec`.

## Considered options

- **`commons`.** ADR-0004 keeps it free of knowledge of CLIs and requires no sibling and no
  dependency. The YAML helper would give every dependent of `commons` the YAML library, the same
  objection ADR-0012 raised against putting the loader there. A JSON-only core in `commons` would
  split one store across two projects.
- **`cli/spec`.** Its root package does no I/O (ADR-0009), and a state file is I/O. A subpackage
  would hand the YAML library to every project that imports `cli/spec`, `update` and `daemon`
  included.
- **`config`.** It loads layered configuration into a struct (ADR-0012). A store of files the tool
  writes itself is a different concern, and `update` would need the whole loader to write a file.
- **`cli/core`.** `update` may not import it (ADR-0003, ADR-0005), and `update`'s state is the first
  consumer (issue #54). A store there is unreachable by the project that needs it first, and each
  consumer would keep its own atomic writer.
- **An `internal` package in `cli/core`.** It cannot be imported by any other project at all.

## Consequences

- **YAML stays out of the shared libraries.** `commons` and `cli/spec` keep their dependency sets
  (ADR-0004, ADR-0012); a project takes the YAML library by requiring `cli/state`.
- **One atomic writer for the framework.** `config edit` uses `Replace` and `WriteFile` rather than a
  copy of the sequence in `cli/core`.
- **No lock yet.** Two processes writing the same file each leave a whole file, and the last rename
  wins. Serialising them is issue #39.
- **A new project to release.** `cli/state` is tagged `source/cli/state/vX.Y.Z` and needs a Worker
  deploy before its first tag (ADR-0001, ADR-0004).

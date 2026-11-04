# `update` keeps its check state through `cli/state`

Status: amends ADR-0008, which limits `update` to `cli/spec` and `commons`, and ADR-0003, which
gives `update` an atomic writer of its own. Settles the question ADR-0018 leaves open.

`update` requires `cli/state` and writes its **Update state** with `state.SaveJSON`, loading it with
`state.LoadJSON`. The writer `cli/state` holds is the one ADR-0018 split out of `cli` so that more
than one project could use it; a second atomic writer inside `update` would repeat its temp-file,
fsync, rename and directory-fsync steps and drift from them.

## The rules

- **The store is a seam.** `Config.StateStore` is a `StateStore` interface, `Load(ctx) (State,
  error)` and `Save(ctx, State) error`. Nil means the JSON file `update-state.json` in the resolved
  `StateDir`, whose directory is created at 0700 and whose file is written at 0600.
- **Missing and corrupt load as empty.** A missing file and a file `cli/state` reports as corrupt
  both load as the zero `State` with a nil error, so a damaged file costs one extra check and never
  an error. An unreadable file is an error.
- **No lock around the file.** The rename makes each save whole, and the last one wins. Serialising
  the check itself is the **Lock** of ADR-0019, taken by whoever runs the check, not by the store.

## Considered options

- **A private writer in `update`**, as ADR-0003 says. Rejected: it duplicates `cli/state`'s
  mechanics for one file, and `cli/state` requires no sibling, so the edge adds no cycle.

## Consequences

- `go.yaml.in/yaml/v3`, which `cli/state` requires, enters `update`'s module graph although
  `update` writes only JSON.
- Releasing `update` needs `cli/state` released first (ADR-0006).

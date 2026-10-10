---
about: "update and cli are deliberately duplicated and decoupled; update must never require cli, and they agree only through the documented `version --json` contract"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - CONTEXT.md
---

Kind: invariant (with rationale).

ADR-0003 fixes the project graph: `daemon -> cli`, `daemon -> update`, `cli` and `update` require no sibling, nothing requires `daemon`. Cycles are forbidden; shared code moves lower in the graph.

A competent engineer would "dedupe" the following, and each is a deliberate duplication:
- `update` keeps its own small platform-dir resolver for the default `StateDir` (overlaps `cli` paths).
- `update` writes state with its own atomic writer, not `cli`'s store, so there is no shared lock or file format.
- `update.Config` takes plain values (Name, EnvPrefix, Version, Source, StateDir, Token) and no `cli` type. The tool's `main` builds both from the same values and passes `app`'s resolved state dir as `StateDir`.
- The staged-binary preflight runs `<staged> version --json`. The JSON shape is defined by `update`; `cli`'s version command must emit it. Changing either side alone breaks self-update verification at runtime, with no compile error, because there is no shared type.

What breaks if wrong: importing `cli` from `update` forces every self-update-only consumer to take the whole shell and its release cadence, and permits reaching into `cli` internals. Rejected options are recorded in the ADR (update inside cli, daemon inside cli, fourth shared project).
Evidence: read of ADR-0003 sections "Dependency direction", "How update takes the app identity...", "Considered options". Note: only `source/cli` exists today (a doc.go stub); update and daemon are design-only.

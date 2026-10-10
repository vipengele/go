---
about: "glossary traps: Env is passed explicitly (never os.Getenv/os.Stdout), Service vs daemon, Runner knows nothing of siblings, 'Release' means a repo project version not a tool's release"
saw:
  - CONTEXT.md
---

Kind: invariant.

CONTEXT.md library vocabulary constrains future design in cli/update/daemon:
- Code takes an `Env` (env vars, std streams, cwd, platform paths); direct `os.Getenv`/`os.Stdout` use defeats test substitution.
- A tool's own releases come from a `Release source` (the `update` `Source` interface: lists releases and serves assets); it does not choose among them; version comparison and asset selection do.
- A `Runner` blocks in start until ctx cancelled, stop returns after release, and knows nothing of siblings or restarts; the `Supervisor` (one per process) starts all, stops in reverse order on shutdown or failure.
- `Service` = the OS registration; `daemon` = the program. The managed binary is also the file the Service starts, which makes update pending/confirm/rollback visible to the Supervisor.
- Avoid-words are deliberate (e.g. "module" only for go.mod talk, never "library" for package).
Evidence: read of CONTEXT.md; none of this is implemented yet (only source/cli/doc.go exists).

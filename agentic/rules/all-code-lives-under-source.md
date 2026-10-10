---
description: Nothing that builds, tests or ships lives at the repo root; it lives in a project under source/.
---

# All code lives under `source/`

Every package, command, script, `go.mod`, `go.sum` and tool config that builds, tests or ships
belongs to a project under `source/<project>/`. The repo root holds only what crosses projects:
`.github/`, `agentic/`, `.memory/`, `.lydite/`, `.serena/`, `.gt.yaml`, `.gt-repo.yaml`, `go.work`,
`docs/`, `CONTEXT.md`, `README.md` and `LICENSE`.

A root `go.mod` would make the root a module of its own: CI's change detection selects projects by
their `source/` directory and would never build or test it, and no tag the release workflow accepts
could version it.

## Applies to

- Any new package, command or tool: it goes in an existing project, or starts a new project under
  `source/<project>/` with its own `go.mod` (ADR-0001).
- Tooling convenience at the root (a root `Makefile` that runs every project, a shared
  `.golangci.yml`): not allowed. Shared config is repeated per project, or lives inside one.

## Example

A linter configuration every project wants is not a root `.golangci.yml`; each project carries its
own `source/<project>/.golangci.yml`, or takes golangci-lint's defaults.

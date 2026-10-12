---
description: Go code that builds, tests or ships lives in a project under source/; tools/ holds repo tooling; nothing else at the repo root builds.
---

# All code lives under `source/`

`source/` holds the Go framework and nothing else: every package, command, script, `go.mod`,
`go.sum` and tool config that builds, tests or ships as part of it belongs to a project under
`source/<path>/` (ADR-0001, ADR-0004). `tools/` holds the repository's own tooling that is not a Go
project, such as the `go.vipengele.org` Worker in `tools/vanity-worker`: it has no `go.mod`, is not
released by tag and is deployed from CI. The repo root otherwise holds only what crosses projects:
`.github/`, `agentic/`, `.memory/`, `.lydite/`, `.serena/`, `.gt.yaml`, `.gt-repo.yaml`, `go.work`,
`docs/`, `tools/`, `CONTEXT.md`, `README.md` and `LICENSE`.

A root `go.mod` would make the root a module of its own: CI's change detection selects projects by
their `source/` directory and would never build or test it, and no tag the release workflow accepts
could version it.

## Applies to

- Any new package, command or library: it goes in an existing project, or starts a new project
  under `source/<path>/` with its own `go.mod` (ADR-0001, ADR-0004).
- Tooling for the repository itself that is not a Go project (a deployment, a generator in another
  language): it goes under `tools/<name>/`, never under `source/`, where every rule that selects
  projects would have to learn to skip it.
- Tooling convenience at the root (a root `Makefile` that runs every project, a shared
  `.golangci.yml`): not allowed. Shared config is repeated per project, or lives inside one.

## Example

A linter configuration every project wants is not a root `.golangci.yml`; each project carries its
own `source/<path>/.golangci.yml`, or takes golangci-lint's defaults.

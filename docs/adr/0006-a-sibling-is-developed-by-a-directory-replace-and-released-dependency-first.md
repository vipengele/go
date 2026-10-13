# A sibling is developed through a directory `replace` and released dependency-first

Status: supersedes ADR-0002.

A project that uses another project's packages requires that project's module in its `go.mod`. While
the two are developed together, the dependent also replaces the module with the sibling's own
project directory:

```
// source/cli/spec/go.mod
require go.vipengele.org/commons v0.0.0

replace go.vipengele.org/commons => ../../commons
```

The `replace` target is the relative path from the dependent's directory to the sibling's project
directory, so it depends on depth: `../commons` from `source/update`, `../../commons` from
`source/cli/spec`. While the `replace` is present the Go command never looks the required version
up. It is `v0.0.0` when the sibling has never been released, or the sibling's last released version.
It stays on the `v0` line: a major version above `v1` without a `/vN` path suffix, such as `v9.9.9`,
makes every go command fail on the `go.mod`.

A change that spans two projects is one pull request. The library and its dependent change
together, and no library is released so that a dependent can proceed. A release is for a feature or
a fix that a consumer should receive, never for unblocking development.

CI keeps `GOWORK=off` and the `go mod tidy` gate. A `require` plus a `replace` of a directory passes
both with no `go.sum` entry for the sibling, and builds the same in a workspace.

## Development mode and strict mode

`verify-sibling-deps` checks every `source/**/go.mod`, and each project is in one of two modes.

A project is in **strict mode** while a file `docs/release-notes/<project>/v<version>.md` exists and
the repository has no tag `source/<project>/v<version>`: its release is written and not yet tagged.
Every other project is in **development mode**.

In development mode the check:

- accepts a `replace` of a sibling whose target, resolved against the `go.mod`'s directory and
  compared by real path, is exactly `source/<sibling>`, and which names no version;
- fails a `replace` that names a version (`=> ../commons v0.1.0`), a `replace` of a sibling to any
  other directory, a sibling required without a `replace` at a version that has no tag, and a
  sibling at a pseudo-version, with or without a `replace`;
- fails a `replace` of a module that is not a sibling, a module path other than
  `go.vipengele.org/<path>` for `source/<path>`, and a project directory holding another `go.mod`.

In strict mode the project's `go.mod` carries no `replace` of any sibling, and every sibling version
it requires has its tag.

The mode is read from the repository's files and its tags, not from a diff against a base, so the
check answers the same on `pull_request`, `merge_group`, `push` and `workflow_dispatch`. Notes for a
version that is already tagged leave the project in development mode.

## Release

Release order is dependency-first, and only at release time. A dependent's release needs every
library it requires released at a version that contains what it uses. A project that nothing
unreleased feeds, such as a patch or a project no sibling uses, is released whenever it is ready.

A release is one pull request: the notes, the `replace` dropped (`go mod edit -dropreplace`), the
sibling raised to its tag (`go get go.vipengele.org/<sibling>@vX.Y.Z`) and `go mod tidy`. The
notes put the project into strict mode, so the pull request's own `GOWORK=off` CI compiles it
against the released siblings, as a consumer of the tag will. The release skill runs the same
check locally before the tag is pushed, and the tag goes on that pull request's merge commit.

`release.yml` repeats the strict check in its `tag` job for the tagged project only, because a pushed
tag is permanent. A sibling that is under development elsewhere in the repository does not block
the release. The `test` job runs `go mod tidy` and fails when it changes `go.mod` or `go.sum`, and
runs `go build ./...` alongside `go vet` and the suite.

## Why

A `replace` of the sibling's directory is the one arrangement that keeps `GOWORK=off` CI and the
`go mod tidy` gate while the library is unreleased. Without it, a placeholder `require` cannot be
resolved: tidy asks `go.vipengele.org` for a version that does not exist, and the build has no
`go.sum` entry for it.

Every consumer ignores a `replace` in a published `go.mod`, so a leftover one builds in this
repository and fails everywhere else. A `GOWORK=off` build still honours the main module's own
`replace`, so building does not catch it. Strict mode does: it is on from the moment the notes land
until the tag exists, which covers every change that could reach the tag.

Change detection derives a dependency edge only from a `require` of `go.vipengele.org/<path>`, and a
change to `go.work` selects no project. The dependent keeps its `require` beside the `replace`, so a
change to the library still selects the dependent in CI.

## What it requires

- **Change detection selects dependents.** A change to a project also selects every project whose
  `go.mod` requires it, directly or transitively, so a break is caught in the project that consumes
  it.
- **`go.work` lists every project.** Adding a project adds its `use` line. The workspace serves
  local development across projects with no `replace` between them; CI never reads it.
- **A sibling version required without a `replace` is a tag.** Never a pseudo-version.

## What this changes in earlier ADRs

- **ADR-0002.** Superseded. A dependent uses an unreleased sibling through a directory `replace`,
  and the rule that every sibling is required at a released version holds for a project from its
  notes to its tag. `GOWORK=off` CI and `go.work` for local development
  stay.
- **ADR-0003.** `daemon` requires `cli` and `update`; while they are developed together it replaces
  them with their directories. A feature spanning `update` and `daemon` is one change, and only
  their releases are ordered, `update` first.

## Considered options

- **Release before use, as ADR-0002 decided.** Keeps every `go.mod` consumer-ready at every commit,
  but a change spanning two projects becomes two pull requests with a release between them, and the
  library is released to unblock its dependent rather than because a consumer needs it.
- **Workspace-only development with a placeholder `require` and no `replace`.** Keeps `go.mod` free
  of `replace`, but fails CI: `go mod tidy` asks `go.vipengele.org` for the placeholder version and
  gets a 404, and `GOWORK=off go build` fails with a missing `go.sum` entry for the sibling.
- **CI in workspace mode.** Builds the dependent against the sibling as it sits on the branch, but
  that is not the build a consumer of the tag gets, and it drops the `go mod tidy` gate's view of
  the module alone.
- **Vendoring sibling modules.** Duplicates source and hides which release a project was verified
  against.
- **Strict mode from the diff: strict when a change adds release notes.** Notes can land several
  changes before the tag, and `push`, `merge_group` and `workflow_dispatch` runs have no base to
  diff against.

## Consequences

- **Notes without a tag lock the project's requires.** From the moment a project's notes land until
  its tag is pushed, it is in strict mode, and a change that develops it together with an
  unreleased sibling fails the check. Tag the release, or move its notes out, before such a change.
- **A `replace` in a published `go.mod` is ignored by consumers.** The module would build here and
  fail for them. Strict mode and the release workflow's `tag` job are what stop one reaching a tag;
  nothing that only builds the module notices it.
- **The tag lookup is unauthenticated.** `git ls-remote` against `https://github.com/<repository>`
  works while the repository is public and fails on a private one.
- **Prerelease and major-version tags fail the `tag` job.** `verify-sibling-deps` accepts only
  `source/<path>/vX.Y.Z`, so a `-rc.N` suffix fails, and it expects the module path
  `go.vipengele.org/<path>`, so a `/v2` module fails.
- **A `/v2` sibling path does not resolve to a directory.** The check maps
  `go.vipengele.org/<path>/v2` to `source/<path>/v2`, which is not a project, and the check has no
  longest-prefix lookup of the owning project there: a sibling on a major version above `v1` is
  reported as untagged or misplaced rather than checked.
- **A release pull request is the only place a dependent's `replace` is dropped.** A dependent that
  is not being released keeps its `replace` for as long as it is developed with the sibling.

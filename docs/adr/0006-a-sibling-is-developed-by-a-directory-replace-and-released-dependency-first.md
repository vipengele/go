# A sibling is developed through a directory `replace` and released dependency-first

Status: supersedes ADR-0002. Amended by `0037-a-release-is-the-merge-of-its-notes.md`, which
replaces the tag pushed after the release pull request and the `tag` job of `release.yml` in the
Release section: merging the notes tags the merge commit.
Amended by `0038-a-dependent-is-released-from-a-release-commit.md`, under which a dependent keeps its
`replace` on `main` in strict mode, requires the sibling at its pending version, and is released from a
release commit that drops the `replace`, so a chain is released by one pull request.

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
- fails a `replace` that names a version on either side (`=> ../commons v0.1.0` and
  `go.vipengele.org/commons v0.1.0 => ../commons`), a `replace` of a sibling to any other directory,
  a sibling required without a `replace` at a version that has no tag, and a sibling at a
  pseudo-version, with or without a `replace`;
- fails a `replace` of a module that is not a sibling, a module path other than
  `go.vipengele.org/<path>` for `source/<path>`, and a project directory holding another `go.mod`.

In strict mode the check accepts a `replace` of a sibling by its own project directory, naming no
version, only while the sibling is required at its pending version, whose notes exist and whose tag
does not, or at one of its tags. A sibling required without a `replace` has its tag. The failures of
development mode apply as well.

The mode is read from the repository's files and its tags, not from a diff against a base, so the
check answers the same on `pull_request`, `merge_group`, `push` and `workflow_dispatch`. Notes for a
version that is already tagged leave the project in development mode.

## Release

Release order is dependency-first, and only at release time: `auto-release.yml` plans the pending
projects as dependency tiers and releases them tier by tier, so a dependent and the siblings it needs
are released from one pull request. A dependent's release needs every library it requires released
at a version that contains what it uses. A project that nothing unreleased feeds, such as a patch or
a project no sibling uses, is released whenever it is ready.

A release is one pull request: the notes of the project and of every sibling released with it, each
sibling required at its pending version or at an existing tag, and `go mod tidy`. The dependent keeps
its directory `replace`, which stays on `main`. The notes put each project into strict mode until its
tag exists. The release skill runs its check locally before the merge, and the merge is the release.
Once `auto-release.yml` has tagged and announced the siblings, the dependent is tagged on a **release
commit**: the merge commit with only the dependent's `go.mod` and `go.sum` changed, its sibling
`replace`s dropped and the module tidied against the siblings' tags. The release commit is on no
branch, and only its tag is pushed.

`release.yml`, which `auto-release.yml` calls for each pending project, runs three jobs. `verify`
(read-only) builds the release commit and validates it for the released project only, because a
pushed tag is permanent: its `go.mod` carries no `replace` and requires every sibling at a tagged
version. No other project's `go.mod` is read, so a sibling that is under development elsewhere in the
repository does not block the release. `verify` then runs, with `GOWORK=off`, `go mod tidy`, failing
when it changes `go.mod` or `go.sum`, and `go build ./...` alongside `go vet` and the suite, so the
dependent is compiled against the released siblings, as a consumer of the tag will compile it. `tag`
(write; runs no project code) rebuilds the identical release commit, requires the commit id `verify`
tested, and pushes only the tag. `announce` asks the module proxy to fetch the version and creates
the GitHub Release.

## Why

A `replace` of the sibling's directory is the one arrangement that keeps `GOWORK=off` CI and the
`go mod tidy` gate while the library is unreleased. Without it, a placeholder `require` cannot be
resolved: tidy asks `go.vipengele.org` for a version that does not exist, and the build has no
`go.sum` entry for it.

Every consumer ignores a `replace` in a published `go.mod`, so a leftover one builds in this
repository and fails everywhere else. A `GOWORK=off` build still honours the main module's own
`replace`, so building does not catch it. The release commit drops the sibling `replace`s, and
`release.yml`'s `verify` job fails a `go.mod` that still carries one, before any tag is pushed.

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
  and the rule that every sibling is required at a released version holds for the `go.mod` at a
  project's tag. From its notes to its tag a project requires each sibling at a tag, or at the
  sibling's pending version under a directory `replace`. `GOWORK=off` CI and `go.work` for local
  development stay.
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
  its tag exists, it is in strict mode, and a change that develops it together with a sibling
  required at a version that is neither pending nor tagged fails the check. Release the sibling in
  the same pull request, merge the release, or move its notes out, before such a change.
- **A `replace` in a published `go.mod` is ignored by consumers.** The module would build here and
  fail for them. The release commit drops the sibling `replace`s, and the release workflow's
  `verify` job, which fails a `go.mod` carrying any `replace` before `tag` runs, is what stops one
  reaching a tag; nothing that only builds the module notices it.
- **The tag lookup is unauthenticated.** `git ls-remote` against `https://github.com/<repository>`
  works while the repository is public and fails on a private one.
- **Prerelease tags fail the `verify` job, and a major-version module fails CI.** `verify` accepts
  only a version of the form `vX.Y.Z`, so a `-rc.N` suffix fails. `verify` accepts a `/vN` module
  path for a `vN.x.y` tag, but `verify-sibling-deps` expects `go.vipengele.org/<path>` for
  `source/<path>`, so a `/v2` module fails CI before its notes can merge.
- **A `/v2` sibling path does not resolve to a directory.** The check maps
  `go.vipengele.org/<path>/v2` to `source/<path>/v2`, which is not a project, and the check has no
  longest-prefix lookup of the owning project there: a sibling on a major version above `v1` is
  reported as untagged or misplaced rather than checked.
- **A release commit is the only place a dependent's `replace` is dropped.** `main` keeps the
  dependent's `replace` for as long as it is developed with the sibling, its releases included.

---
description: A project developed with a sibling requires it and replaces it with the sibling's own directory; the release commit drops the replace and the dependent is tagged on it.
---

# A sibling is developed by a directory replace

When `source/b` uses packages from `source/a` and the two are developed together, `source/b/go.mod`
requires `go.vipengele.org/a` and replaces it with `a`'s own project directory (ADR-0006, ADR-0004):

```
// source/cli/spec/go.mod
require go.vipengele.org/commons v0.0.0

replace go.vipengele.org/commons => ../../commons
```

The target is the relative path from `b`'s directory to `a`'s, so it depends on depth: `../commons`
from `source/update`, `../../commons` from `source/cli/spec`. The `replace` names no version on
either side, and `verify-sibling-deps` fails one that resolves anywhere but `source/a`. The
`require` stays beside it: change detection derives a dependency edge only from a `require`, so
without it a change to `a` does not select `b` in CI.

While the `replace` is present the required version is never looked up. It is `v0.0.0` when `a` has
never been released, or `a`'s last released version, and it stays on the `v0` line: a version such
as `v9.9.9`, a major above `v1` with no `/vN` suffix, makes every go command fail on the `go.mod`.
A pseudo-version is refused with or without a `replace`.

A change that spans two projects is one pull request. No library is released so that a dependent
can proceed; a release is for a feature or a fix a consumer should receive.

Every consumer of `b` ignores a `replace` in its published `go.mod`, and a `GOWORK=off` build here
still honours it, so a leftover `replace` builds in this repository and breaks everyone else. Only
the explicit check catches it:

- **Strict mode.** While some `docs/release-notes/b/v<version>.md` exists and the repository has no
  tag `source/b/v<version>`, `b` is in strict mode: a sibling may keep its directory `replace` if
  `b` requires it at the sibling's pending version (its notes exist, its tag does not) or at a tag
  of the sibling, and a sibling without a `replace` is required at a tagged version. Notes for a
  version that is already tagged leave `b` in development mode.
- **The release commit.** A `replace` on `main` never reaches a tag. `release.yml` builds, for `b`,
  a commit on the merge commit that changes only `source/b/go.mod` and `go.sum`: the `replace`s
  dropped, tidied against the siblings' tags, subject `chore(release): b <version>`. `verify`
  validates and tests it, and `tag` rebuilds the identical commit, requires the same id and pushes
  only the tag (ADR-0038). `main` keeps the `replace`, and `verify` fails when the tidied go.mod
  still carries one or requires a sibling at an untagged version.

## Applies to

- Starting to use a sibling: `go mod edit -require=go.vipengele.org/a@v0.0.0` (or `a`'s last
  release) and `go mod edit -replace=go.vipengele.org/a=../a`, with the path from `b`'s depth, then
  `go mod tidy`.
- Releasing `b` together with `a`: one release pull request carries the notes of both, and `b`
  keeps its `replace` and requires `a` at `a`'s pending version
  (`go mod edit -require=go.vipengele.org/a@vX.Y.Z`, then `go mod tidy`). `auto-release` tags `a`,
  then `b` on a release commit that drops the `replace`. A dry run of the chain fails at `b`,
  because it tags nothing.
- Releasing `b` when `a` is already tagged: the same pull request carries `b`'s notes and requires
  `a` at its tag; the `replace` may stay or be dropped with `go mod edit -dropreplace` and
  `go mod tidy`. The local check in the `release` skill is mandatory before merging, because the
  merge of the notes is the release.
- Release order is dependency-first at release time only: `b`'s release needs `a` released at a
  version that contains what `b` uses, in the same pull request or before it. Any project nothing
  unreleased feeds is released whenever it is ready.
- A chain stopped halfway, `a` tagged and `b` failed, still passes CI, since `b` requires `a` at a
  tag. The fix is an ordinary pull request.
- The root `go.work` lists every project for local development. CI runs with `GOWORK=off` and never
  reads it.

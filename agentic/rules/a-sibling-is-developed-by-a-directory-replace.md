---
description: A project developed with a sibling requires it and replaces it with the sibling's own directory; the release PR drops the replace and requires the sibling at a tag.
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
  tag `source/b/v<version>`, `b` is in strict mode: its `go.mod` replaces no sibling and requires
  every sibling at a version tagged `source/<sibling>/vX.Y.Z`. Notes for a version that is already
  tagged leave `b` in development mode.
- **The release workflow.** `release.yml`'s `tag` job repeats the strict check for the tagged
  project alone, so a sibling developed elsewhere in the repository never blocks the release.

## Applies to

- Starting to use a sibling: `go mod edit -require=go.vipengele.org/a@v0.0.0` (or `a`'s last
  release) and `go mod edit -replace=go.vipengele.org/a=../a`, with the path from `b`'s depth, then
  `go mod tidy`.
- Releasing `b`: one release pull request carries the notes, `go mod edit -dropreplace`,
  `go get go.vipengele.org/a@vX.Y.Z` and `go mod tidy`. Its `GOWORK=off` CI compiles `b` against the
  released `a`, as a consumer of the tag will. The local pre-tag check in the `release` skill is
  mandatory, and the tag goes on that pull request's merge commit.
- Release order is dependency-first at release time only: `b`'s release needs `a` released at a
  version that contains what `b` uses. Any project nothing unreleased feeds is released whenever it
  is ready.
- A project in strict mode cannot take a sibling `replace` until its tag is pushed. Tag the release,
  or move its notes out, before developing it with an unreleased sibling.
- The root `go.work` lists every project for local development. CI runs with `GOWORK=off` and never
  reads it.

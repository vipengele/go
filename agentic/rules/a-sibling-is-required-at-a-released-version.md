---
description: A project requires a sibling at a tagged release, never through replace or a pseudo-version; go.work is for local development only.
---

# A sibling is required at a released version

When `source/b` uses packages from `source/a`, `source/b/go.mod` requires
`github.com/vipengele/go/source/a` at a version that has the tag `source/a/vX.Y.Z` (ADR-0002). A
`replace` of a sibling is ignored by every consumer of `b` and breaks them; a pseudo-version names
a commit instead of a release.

Local development of both together uses the root `go.work`, which is not part of any module.

## Applies to

- A change spanning two projects is two changes: merge and release `a`, then raise `b`'s `require`
  to the new tag in the change that uses it.
- `go get github.com/vipengele/go/source/a@vX.Y.Z` in `b` rather than editing `go.mod` by hand, so
  `go.sum` follows.
- CI runs with `GOWORK=off`. If a project builds only with the workspace active, it requires a
  sibling version that does not contain what it uses.

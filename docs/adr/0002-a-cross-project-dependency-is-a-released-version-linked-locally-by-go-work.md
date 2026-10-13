# A cross-project dependency is a released version, linked locally by `go.work`

Status: superseded by ADR-0006.

A project that uses another project's packages names the module in its `go.mod` at a tagged
release, and nothing else:

```
// source/example/go.mod
require github.com/vipengele/go/source/cli v0.3.0
```

A `go.mod` never carries a `replace` for a sibling and never requires one at a pseudo-version.

For development, a `go.work` at the repo root lists every project:

```
go 1.27

use (
	./source/cli
	./source/example
)
```

While a workspace is active the Go command resolves each listed module from its directory, so an
edit to a library is visible to its consumer without a release. `go.work` is not part of any
module, so it never reaches a consumer.

CI and the release workflow set `GOWORK=off`. Each project builds and tests from its own `go.mod`
and `go.sum`, with every sibling at the released version it requires — the build a consumer of the
tag gets. `verify-sibling-deps` fails a `replace` of a sibling, a sibling at a pseudo-version, and
a sibling version that has no tag in this repository.

## Why

A `replace` in a published `go.mod` is ignored by every consumer of that module, so the module
builds in this repo and fails everywhere else. A pseudo-version names a commit rather than a
release and makes the dependent unreproducible from a tag. The workspace gives local development
what `replace` would, outside the file consumers read.

CI without the workspace is what makes the first two checkable: a module that builds only because
`go.work` supplies a sibling fails CI the same way it would fail for a consumer.

## What it requires

- **A dependency is released before its dependent.** A change that spans two projects is two
  changes: the library is merged and tagged, then the dependent's `require` is raised to that tag
  and the dependent is changed. The `release` skill walks the order.
- **Change detection selects dependents.** A change to a project also selects every project whose
  `go.mod` requires it, directly or transitively, so a break is caught in the project that consumes
  it.
- **`go.work` lists every project.** Adding a project adds its `use` line.

## Considered options

- **`replace` directives for siblings.** Works in the repo, breaks every consumer.
- **CI in workspace mode.** Tests the code as it sits on the branch, which is not the code a
  consumer receives from the tag.
- **Vendoring sibling modules.** Duplicates source and hides which release a project was verified
  against.

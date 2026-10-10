# Vipengele Go

The Go libraries of the vipengele platform: reusable modules for building Go services and
command-line tools, each versioned and released on its own.

## Language

**Project**:
A self-contained Go module under `source/<project>/`, with the module path
`github.com/vipengele/go/source/<project>`. A project is released alone, by the tag
`source/<project>/vX.Y.Z` (ADR-0001).
_Avoid_: module in prose about the repo's layout (the Go toolchain's word, used when talking about
`go.mod` itself), repo, package

**Package**:
One Go package inside a project, imported as `github.com/vipengele/go/source/<project>/<package>`.
Code a project does not export lives in `internal/`.
_Avoid_: library (ambiguous between package and project)

**Release**:
One project at one version, identified by its tag. The tag is the publication: the module proxy
serves the version from it, and the `release` workflow only validates, tests and announces it.
_Avoid_: publish (nothing is uploaded)

**Prefixed tag**:
The tag form `source/<project>/vX.Y.Z`. Go resolves a module in a subdirectory only from a tag
that starts with its directory path.
_Avoid_: version tag (a bare `vX.Y.Z` is not a release here)

**Workspace**:
The root `go.work`, which lists every project so local development resolves siblings from disk
(ADR-0002). CI never uses it.
_Avoid_: monorepo module

**Sibling**:
Another project in this repo that a project requires. A sibling is always required at a released
version, never through `replace` (ADR-0002).
_Avoid_: local dependency

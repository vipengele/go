# Each project is a Go module, released by its own path-prefixed tag

The repo is `vipengele/go`: the Go libraries of the vipengele platform. Inside it, each **project**
lives in `source/<project>/` as its own Go module — its own `go.mod`, `go.sum`, linter config and
tests. The module path is the repository path plus the directory:

```
github.com/vipengele/go/source/<project>
```

A project holds one or more public Go packages as subdirectories, so an import path reads
`github.com/vipengele/go/source/<project>/<package>`. Code a project does not mean to export lives
under its `internal/`.

Everything that builds, tests or ships lives under `source/`. The repo root holds only what crosses
projects: `.github/`, `agentic/`, `.memory/`, `.lydite/`, `.serena/`, the gt config, `docs/`,
`CONTEXT.md` and, once a project exists, `go.work`.

A release is one tag per project, `source/<project>/vX.Y.Z`, and it releases that project alone. The
Go toolchain resolves a module that lives in a subdirectory from a tag whose name is the
subdirectory path followed by the version, so this is the only tag form the module proxy accepts
for a nested module. A human pushes the tag; the `release` workflow validates it, runs the module's
suite, asks the module proxy to fetch the version and creates the GitHub Release. There is no
registry upload and no publishing credential, because the tag is the publication.

## Why a module per project

A module is Go's unit of versioning, dependency resolution and compatibility. A consumer of the
auto-update library should not pull the dependency graph of every other library in the repo, and
should be able to take a patch of one without a bump of the rest. Separate `go.mod` files give each
project its own dependency set and its own `go` directive.

## Why the module path contains `source/`

Go requires a nested module's path to end with its directory relative to the repository root, or
the proxy cannot find it. The `source/` prefix is the price of the layout rule that no code lives at
the repo root; the alternative is modules at the root (`cli/`, `core/`), which that rule rejects.

## Why a project releases alone

Go's compatibility promise is per module: a module on `v1` and above keeps its API, and a consumer
selects the minimum version satisfying all requirements. Releasing every project at every tag would
publish versions that carry no change and make a consumer's `go.sum` churn for nothing. Where one
project depends on another, ADR-0002 fixes how the pair is released.

## Considered options

- **One repo-wide tag `vX.Y.Z`, as vipengele/typescript does.** Go cannot resolve a nested module
  from an unprefixed tag; only a module at the repository root can use it.
- **Lockstep prefixed tags: one release script tags every module at one version.** Keeps one
  version for the framework, at the cost of a new version for projects with no change.
- **One module at the root with every library a package of it.** Simplest tagging, but every
  consumer depends on every library's dependencies, and one library cannot reach `v1` before
  another.

## Consequences

Dependabot needs a `gomod` entry per project directory, and CI selects projects from a diff. A
project's version history is read from `git tag --list 'source/<project>/v*'`. Release notes live in
`docs/release-notes/<project>/vX.Y.Z.md`, one file per tag.

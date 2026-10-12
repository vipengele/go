---
about: "changed-projects finds a project at any depth under source/, names it by its path relative to source/, gives a changed file to the project with the longest matching directory, counts only a require of go.vipengele.org/<path> as an edge, and fails on a go.mod inside another project's directory"
saw:
  - .github/actions/changed-projects/action.yml
  - .github/actions/verify-sibling-deps/action.yml
  - .github/workflows/ci-build.yml
  - .github/workflows/ci-test.yml
---

`.github/actions/changed-projects/action.yml:53` lists every `go.mod` under `source/` with `find`, so a
`go.mod` at `source/<dir>/<sub>` makes the project `<dir>/<sub>`. A `go.mod` in a `testdata/` or
`vendor/` directory is taken for a project too, and since a project directory must not contain another
`go.mod` (`:61-69`, and the same pairwise test in `verify-sibling-deps/action.yml`) it fails the
action. The jq function `owner` (`:103`) returns the identifier that is the longest prefix of a path
at a `/` boundary; a changed file selects its owner (`:107`), so `source/<dir>/<sub>/x.go` selects
`<dir>/<sub>`, never a project at `source/<dir>`, and a file in a directory that holds projects but is
no project selects nothing.

The dependency edges (`:116-120`) select only a require path that starts with `go.vipengele.org/`,
strip that prefix and resolve the rest with `owner`, so a `/vN` suffix still resolves to its project.
A require of the repository path `github.com/vipengele/go/source/<path>` is not an edge: a module is
named by its vanity path (ADR-0004), and `verify-sibling-deps` fails a module line that is not
`go.vipengele.org/<path>`. `ci-build.yml` and `ci-test.yml` build `source/${{ matrix.project }}`, so an
identifier with a `/` works as a directory, a job name and a `go-version-file` path. `go mod edit -json`
rejects a `/v2` module path at a `v0.x` version, so a fixture for that case needs a `v2` version.

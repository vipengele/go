---
about: "changed-projects finds a project at any depth under source/, names it by its path relative to source/, gives a changed file to the project with the longest matching directory, and counts a require of github.com/vipengele/go/source/<path> or go.vipengele.org/<path> as an edge"
saw:
  - .github/actions/changed-projects/action.yml
  - .github/workflows/ci-build.yml
  - .github/workflows/ci-test.yml
---

`.github/actions/changed-projects/action.yml:47` lists every `go.mod` under `source/` with `find`, so a
`go.mod` at `source/<dir>/<sub>` makes the project `<dir>/<sub>`, and a `go.mod` in a `testdata/` or `vendor/`
directory would be taken for a project too. The jq function `owner` (`:82`) returns the identifier
that is the longest prefix of a path at a `/` boundary; a changed file selects its owner (`:87`), so
`source/<dir>/<sub>/x.go` selects `<dir>/<sub>`, never a project at `source/<dir>`.

The dependency edges (`:99-100`) strip either `github.com/vipengele/go/source/` or `go.vipengele.org/`
from a require path and resolve the rest with `owner`, so a `/vN` suffix still resolves to its
project. `ci-build.yml` and `ci-test.yml` build `source/${{ matrix.project }}`, so an identifier with a
`/` works as a directory, a job name and a `go-version-file` path. `go mod edit -json` rejects a
`/v2` module path at a `v0.x` version, so a fixture for that case needs a `v2` version.

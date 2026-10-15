---
about: "a dependent's go.mod only gains golang.org/x/mod when it imports a cli/spec package that imports it; go mod tidy in cli/spec drops the require until some package imports x/mod"
saw:
  - source/cli/spec/go.mod
  - source/cli/core/go.mod
  - source/update/go.mod
  - docs/adr/0010-version-comparison-lives-in-cli-spec-version.md
---

- Module graph pruning: `cli/core/go.mod` carries `golang.org/x/mod v0.42.0 // indirect` because
  `buildinfo` imports `cli/spec/version`. `source/update/go.mod` has no x/mod and `update` has no
  go.sum, because nothing it imports reaches x/mod. It gains an indirect require, and a go.sum, the
  commit that first imports `cli/spec/version` (or any package that does), or CI's `go mod tidy`
  cleanliness check fails there.
- `go mod tidy` in `cli/spec` removes the `golang.org/x/mod` require while no package imports it, so
  the require and the code that imports it land in one commit.

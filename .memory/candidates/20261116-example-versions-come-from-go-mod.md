---
about: "the greet example's tests read sibling versions from its go.mod, not runtime/debug build info: under go.work (lydite, a plain local go test) the build info reports (devel) for every sibling, and modfile.ParseLax silently drops replace lines"
saw:
  - source/examples/greet/guide_test.go
  - source/examples/greet/release_test.go
  - go.work
  - .lydite/components.yml
---

- `source/examples/greet` requires cli/core, cli/spec, config and update at their tags with no `replace`, and is listed in `go.work`. With the workspace active, each sibling resolves from the working tree and `debug.ReadBuildInfo()` reports its version as `(devel)` with `Replace == nil`, so a version check built on build info fails in workspace mode though go.mod is correct (`cd source/examples/greet && go test ./...` without `GOWORK=off`).
- `requiredVersions` in `guide_test.go` therefore parses `./go.mod` with `golang.org/x/mod/modfile`. It must use `modfile.Parse`: `modfile.ParseLax` ignores `replace` directives, and the replace check then passes on a go.mod that carries one.
- The replace check in `requiredVersions` is the only guard against a sibling `replace` in an example: verify-sibling-deps accepts a directory replace of a sibling for any project with no release notes, and an example never has any.
- Leaving the example out of `go.work` is no way round this: from inside a module that is not one of the workspace's, the go command refuses to run.

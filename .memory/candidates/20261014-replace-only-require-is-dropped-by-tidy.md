---
about: "A dependent whose go.mod has a sibling require and a directory replace but no code importing the sibling loses the require on go mod tidy, so the CI tidy gate fails until the first import lands"
saw:
  - source/cli/spec/go.mod
  - source/cli/spec/identity.go
  - .github/workflows/ci-build.yml
---

`source/cli/spec/go.mod` carries `require go.vipengele.org/commons v0.0.0` and
`replace go.vipengele.org/commons => ../../commons` (ADR-0006). While `source/cli/spec` held only
`doc.go`, `GOWORK=off go mod tidy` exited 0 but removed the `require` and kept the `replace`, so the
scaffold's `go.mod` was not tidy-clean: the `ci-build.yml` step that runs `go mod tidy` and
`git diff --exit-code go.mod go.sum` would fail on it. Once `source/cli/spec/identity.go` imports
`go.vipengele.org/commons/validate`, tidy leaves `go.mod` unchanged and needs no `go.sum` entry for
the directory replace. A project that adds a sibling `require` and `replace` therefore has to land
the first import in the same pull request. A scaffold commit alone is not tidy, and only the pull
request's head is checked.

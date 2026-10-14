---
about: "buildinfo's -X variables only take effect while they stay uninitialised package-level strings that Read references, and the link-time test builds an explicit path into testdata because ./... skips it"
saw:
  - source/cli/core/buildinfo/buildinfo.go
  - source/cli/core/buildinfo/doc.go
  - source/cli/core/buildinfo/ldflags_test.go
---

`go build -ldflags "-X go.vipengele.org/cli/core/buildinfo.version=..."` sets `version`, `commit` and
`date` in `buildinfo.go` only while each is a string variable with no non-constant initialiser and the
program references it; otherwise the linker ignores the flag with no error. Renaming `version` makes
`TestLinkTimeValues` in `ldflags_test.go` fail (the binary prints `dev`), which is what the test is for.

The test builds `./buildinfo/testdata/ldflagsmain` with `cmd.Dir` set to the module directory and
`-buildvcs=false`. `go vet`, `go build` and golangci-lint with `./...` skip `testdata`, an explicit path
into it does not, and `gofmt -l .` does not skip it. The test fails, rather than skips, when `go` is not
on PATH, and skips only under `testing.Short()`.

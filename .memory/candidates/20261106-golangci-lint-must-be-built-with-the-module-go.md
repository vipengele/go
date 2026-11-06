---
about: "golangci-lint refuses a module whose go directive is newer than the Go it was built with; with go 1.27.3 in source/*/go.mod, a v2.14.0 built by Go 1.25 or 1.26 cannot lint, so build the pinned version with GOTOOLCHAIN=go1.27.3"
saw:
  - .github/workflows/ci-build.yml
  - source/daemon/go.mod
---

- Projects name `go 1.27.3` (e.g. source/daemon/go.mod), and CI pins golangci-lint v2.14.0 (.github/workflows/ci-build.yml). A golangci-lint binary built with an older Go refuses to load such a module, so a local `golangci-lint run` fails before linting anything.
- `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0` with `GOTOOLCHAIN=auto` builds it with v2.14.0's own toolchain directive (Go 1.26.10), which fails the same way; build it with `GOTOOLCHAIN=go1.27.3` to get a binary that lints these modules.

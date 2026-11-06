---
about: "A golangci-lint built with an older Go than a project's go directive refuses to load; CI pins v2.14.0"
saw:
  - .github/workflows/ci-build.yml
  - source/daemon/go.mod
---

- CI runs golangci-lint `v2.14.0` (`.github/workflows/ci-build.yml:90`). Projects carry
  `go 1.27.3` (`source/daemon/go.mod:3`). A golangci-lint binary built with Go 1.25 fails with
  "the Go language version (go1.25) used to build golangci-lint is lower than the targeted Go
  version (1.27.3)", so a local lint run proves nothing until the binary matches.
- Installing the pin needs a Go >= 1.26 toolchain (`GOTOOLCHAIN=go1.27.3 go install
  github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`); `GOTOOLCHAIN=local` outside a
  project dir can resolve an older system Go and refuse.

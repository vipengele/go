---
about: "go directive must be a full patch version (go 1.27.2), not go 1.27, in every go.mod and go.work; go mod init writes the local toolchain which may be stale"
saw:
  - agentic/rules/go-directive-names-a-patched-release.md
  - go.work
  - source/cli/go.mod
---

Kind: gotcha.

`go 1.27` means 1.27.0, and CI's `actions/setup-go` installs exactly the toolchain the project's go.mod names, so tests run against a stdlib with all later security fixes missing and lydite's `govulncheck` reports stdlib findings. `go mod init` writes the *local* toolchain version, which can be older than the latest patch, so a freshly scaffolded project fails the vulnerability gate. Fix: set the version the finding names (check https://go.dev/dl/?mode=json) in every `source/<project>/go.mod` AND root `go.work`.

Trap: ADR-0002's example `go.work` shows `go 1.27` and ADR examples are not authoritative; the real go.work and source/cli/go.mod use `go 1.27.2`. The rendered CLAUDE.md pasted into some contexts omits this rule; the source is agentic/rules/go-directive-names-a-patched-release.md.
Evidence: read of the rule, ADR-0002, go.work, source/cli/go.mod.

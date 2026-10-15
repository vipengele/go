---
about: "buildinfo stays in cli/core and only its release rule lives in cli/spec/version; moving buildinfo into cli/spec is rejected in ADR-0010 because the ldflags path is the package path"
saw:
  - docs/adr/0010-version-comparison-lives-in-cli-spec-version.md
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - source/cli/core/buildinfo/buildinfo.go
  - source/cli/core/buildinfo/doc.go
  - source/update/.golangci.yml
---

- `buildinfo` sits in `cli/core` because `cli/core` holds the `version` command (ADR-0007). ADR-0010
  records the rejected move into `cli/spec`: the `-X go.vipengele.org/cli/core/buildinfo.version=...`
  ldflags path is the package path, so a move changes it in every consumer's build, plus
  `testdata/ldflagsmain` and `ldflags_test.go`, and invites the shared Go type ADR-0007 rejects.
- `Info.IsDev` is `!specversion.IsRelease(i.Version)` (buildinfo.go). The import is aliased
  `specversion` because the package-level link-time var `version` would clash with a plain import.
- `update` cannot import `cli/core`: its depguard rule `core-stays-out` (source/update/.golangci.yml)
  denies `go.vipengele.org/cli/core/**`. That is why the release rule is in `cli/spec/version` and
  not left in `buildinfo`.
- buildinfo depends on the standard library and `cli/spec/version` (doc.go), not the standard library
  alone.

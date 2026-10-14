---
about: "update.Config embeds spec.Identity, so update requires cli/spec and commons but never imports cli/core, and cobra is allowed only under updatecmd; ADR-0008 amends ADR-0003's 'update requires no sibling'"
saw:
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
  - source/update/config.go
  - source/update/go.mod
  - source/update/.golangci.yml
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
---

- `update.Config` embeds `spec.Identity` (`source/update/config.go`), so `source/update/go.mod` requires `go.vipengele.org/cli/spec` and `go.vipengele.org/commons`, each with a version-less directory replace (`../cli/spec`, `../commons`). ADR-0008 records that this amends ADR-0003 (update requires no sibling; a self-update-only tool takes nothing else) and settles the question ADR-0004 left open.
- The opt-out variable is derived, not configured: `Identity.EnvVar("NO_UPDATE_CHECK")` (`optOutSuffix` in `config.go`). `Config` has no `OptOutEnv` field.
- The boundary is lint, not a test: `source/update/.golangci.yml` enables `depguard` (denies `go.vipengele.org/cli/core/**` everywhere and `github.com/spf13/cobra` outside `**/updatecmd/**`) and `gochecknoglobals`, which also lints `_test.go` files, so tables live inside test functions.
- `Identity` stays out of `commons` and a fourth identity project was rejected (ADR-0008 "Considered options").

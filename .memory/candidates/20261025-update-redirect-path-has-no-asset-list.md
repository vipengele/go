---
about: "update's default GitHub source returns nil Assets on the redirect path, so asset selection cannot rely on Release.Assets; AssetTemplate/ChecksumsAsset already exist as opaque strings"
saw:
  - source/update/github.go
  - source/update/source.go
  - source/update/config.go
  - docs/adr/0016-the-default-update-source-reads-the-github-latest-redirect-then-the-api.md
  - source/update/AGENTS.md
---

- Redirect path returns tag + page URL with nil Assets ("the assets are not listed"): github.go:64 doc, ADR-0016 rule "Release.Assets == nil means the assets are not listed". API path and IncludePrereleases path always return non-nil Assets (github.go:194-212).
- Config already has AssetTemplate (config.go:43-45, default "{name}_{os}_{arch}.tar.gz", opaque string, config.go:19-22) and ChecksumsAsset (config.go:46-48, "checksums.txt"). Placeholders are {name} {os} {arch}; no version, no extension; nothing consumes them yet (grep AssetTemplate only hits config.go/config_test.go).
- update is untagged and has no docs/release-notes/update/, so not in strict mode; only commons has notes (v0.1.0). No other project requires go.vipengele.org/update (grep of go.mod files). No goreleaser/release-binary tooling in the repo (release.yml tags/announces modules only).
- Conventions: every outside-world reach is a Config field resolved in resolve (AGENTS.md:35-37); no mutable package globals (gochecknoglobals; AGENTS.md:38); depguard bans cli/core and cobra outside updatecmd (AGENTS.md:39-40).

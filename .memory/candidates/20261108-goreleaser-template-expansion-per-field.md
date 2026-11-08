---
about: "GoReleaser expands some config fields as templates and others not; .Version drops the v; a binary-format asset gets .exe on windows"
saw:
  - source/update/goreleaser/render.go
  - source/update/goreleaser/goreleaser.yaml.in
  - source/update/goreleaser/roundtrip_test.go
  - docs/adr/0029-a-tool-renders-its-goreleaser-config-from-its-update-config.md
---

What a GoReleaser v2 config field does with its value decides how update/goreleaser must escape it.

- `builds[].binary` and `ldflags` go through text/template; `signs[].args` and `signature` go
  through `os.Expand` and then text/template. `main`, `dir` and `project_name` are not templated.
  `templateLiteral` (render.go) therefore writes `{` and `$` as template actions for the binary,
  the link variables and the signing key path only. Archive and checksum `name_template`s are
  already escaped by `asset.GoReleaserArchive`/`GoReleaserChecksum`, which escape `{` only.
- `{{ .Version }}` is the tag without its leading `v`; a binary linked with it fails
  `cli/spec/version.IsRelease` and never updates itself. The ldflags use `{{ .Tag }}` (ADR-0029).
- With `formats: [binary]`, GoReleaser names the asset `name_template + binary.Ext()`, which is
  `.exe` on windows (internal/pipe/archive/archive.go upstream), so a raw-binary AssetTemplate
  cannot target windows: `ErrRawWindowsAsset`. An archive is named `<name>.<format>`, so each
  format equals `AssetExt`.
- The round-trip test (roundtrip_test.go) reproduces this naming: archive name_template with
  ProjectName/Version/Tag/Os/Arch plus `.<format>` unless binary; the checksum template with no
  Os/Arch; the signature as `os.Expand("${artifact}.minisig")`.

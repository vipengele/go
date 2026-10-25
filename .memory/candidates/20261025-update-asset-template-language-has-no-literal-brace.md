---
about: "update's asset template language has no literal brace, Render and the goreleaser functions accept different languages, and goreleaser's format must equal Config.AssetExt"
saw:
  - source/update/asset/asset.go
  - source/update/asset/doc.go
  - source/update/config.go
---

- Every `{` in a template opens a placeholder (`parse` in asset.go): `{{name}}` is the unknown placeholder `{name`, and `a{` is unclosed. A template can never hold a literal brace; only the binary name inlined into a goreleaser name_template can, and `writeLiteral` escapes each `{` on its own as `{{ "{" }}`. Escaping the pair `{{` is not enough: a name ending in `{` followed by `{{ .Os }}` would still open a broken action.
- `asset.Render` accepts `{ext}` anywhere; `GoReleaserArchive` accepts it only as the trailing `.{ext}` (goreleaser's `format` appends the extension itself) and returns `ErrMisplacedExt` otherwise; `GoReleaserChecksum` rejects any `{ext}`. A template valid for Render may therefore have no goreleaser equivalent (asset/doc.go).
- goreleaser's archive `format` must equal `Config.AssetExt`, or the published name and the one `Config.Select` looks for differ. An archive template with no `{ext}` at all passes through `GoReleaserArchive` unchanged, which suits `format: binary` and nothing else.
- `Config.Validate` validates the archive template with `asset.Validate` and the checksums template with `asset.ValidateChecksums` (no `{ext}`); empty fields mean "use the default" and are skipped. So the render errors inside `Config.Select` are unreachable after `resolve`.

---
about: "planning #65 (goreleaser config in update): ADR-0023 render pattern, no tag-prefix/main-path/ldflags-path data on Config, update may not import cli/core (lint on import path only), per-OS ext impossible, minisig name is fixed suffix"
saw:
  - docs/adr/0023-the-install-script-is-rendered-by-update-installscript-around-the-completion-snippet.md
  - docs/adr/0024-release-assets-are-authenticated-by-a-minisign-signature-over-the-checksums-file.md
  - docs/adr/0026-a-release-is-refused-unsigned-unless-the-consumer-opts-out.md
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
  - source/update/installscript/render.go
  - source/update/config.go
  - source/update/select.go
  - source/update/asset/asset.go
  - source/update/.golangci.yml
  - source/cli/core/buildinfo/doc.go
  - source/update/goreleaser/render.go
  - source/update/go.mod
---

Found while planning issue #65 (goreleaser config template).

- installscript pattern (render.go:72): `Render(cfg update.Config) ([]byte, error)`, calls `cfg.Resolved()` (config.go:438; defaults exist once, ADR-0023 "Defaults exist once"), embedded `*.sh.in` with `@@TOKEN@@` placeholders replaced by one `strings.NewReplacer` pass (render.go:126, no re-scan), optional regions `@@SIGNED_BEGIN@@/END` dropped under AllowUnsigned. Tests: render_test.go string asserts on the output (no golden files; testdata holds only fakeminisign) plus script_test.go running sh against an updatetest server. Typed error ErrZipAsset (errors.go).
- Config has NO tag prefix, main-package path, ldflags path, goreleaser or per-OS ext field. Archive/checksum/signature data: AssetTemplate, AssetExt (single ext, "carries no per-OS rules", config.go:85-89), ChecksumsAsset, TrustedKeys, AllowUnsigned, BinaryName, embedded Identity (Name, Owner, Repo, EnvPrefix, DirName). Version is a runtime field, not a release-config one.
- Signature asset = rendered ChecksumsAsset + fixed ".minisig" (select.go:21,74); not templated (ADR-0024 "The suffix is fixed"). Under AllowUnsigned no signature. ADR-0024 Consequences gives the GoReleaser `signs:` block (artifacts: checksum, cmd: minisign, signature "${artifact}.minisig") as the pipeline's job; update has no signing code and no config field for it. Whether a config template emits `signs:` is a #65/#66 decision; TrustedKeys cannot say it (keys are public).
- ADR-0008:17 and source/update/.golangci.yml depguard `core-stays-out` deny importing go.vipengele.org/cli/core; update/go.mod has no cli/core require. depguard matches import paths, so the literal string `go.vipengele.org/cli/core/buildinfo.version` in update's output breaks nothing recorded. update carries it as the string constants `goreleaser.DefaultVersionVar`/`DefaultCommitVar`/`DefaultDateVar` (source/update/goreleaser/render.go:17-21); a rename in buildinfo is not caught at compile time (ADR-0029). Config.Version is runtime-set by the tool (nothing in update reads buildinfo).
- asset_test.go:10-40,278-296 already runs GoReleaserArchive output through text/template with fake ProjectName/Version/Tag/Os/Arch data and compares to asset.Render (appending "."+DefaultExt when the template had {ext}). The #65 acceptance test (render config, parse names back via Config.Select) can reuse that pattern.
- ADR-0021 extraction supports tar.gz, tgz, zip, raw; but ADR-0023's installscript refuses zip (ErrZipAsset), so a goreleaser config for zip yields assets with no install.sh.
- YAML: update's production code imports no yaml lib; go.yaml.in/yaml/v3 is a direct require only because goreleaser/*_test.go parse the rendered config with it (source/update/go.mod); config and cli/state require it directly. ADR-0012:71-87 says the YAML dependency is carried by `config` alone so dependents do not inherit it; no ADR bans it in update, but the same reasoning (dependency weight on every tool, ADR-0003) applies, and the asset package emits strings by hand.
- No goreleaser v2 schema / `formats` vs `format` notes exist anywhere in docs, ADRs or memory; asset/doc.go says only "format must equal Config.AssetExt".

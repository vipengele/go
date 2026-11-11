---
about: "releasetest.Verify reads dist/artifacts.json (built platforms) and dist/metadata.json (tag), so any artifact handed to it must carry those GoReleaser bookkeeping files, not only the release assets"
saw:
  - source/update/internal/releasetest/verify.go
  - source/update/internal/releasetest/doc.go
  - .github/workflows/release-tool.yml
---

- `Verify` (`source/update/internal/releasetest/verify.go`) takes the platforms built from the `"type":"Binary"` entries of `dist/artifacts.json`, not from the checksum names: a platform whose archive GoReleaser named differently from what `Config.Select` renders would otherwise leave nothing to notice (rationale in `doc.go`).
- The tag check reads `dist/metadata.json`'s `tag`: the default AssetTemplate holds no `{tag}`, so archive names alone cannot attest it. A `--snapshot` build writes the latest git tag there, not the intended one.
- `.github/workflows/release-tool.yml` therefore uploads every regular file at the top of dist in its `release-dist` artifact; its `publish` job leaves out `artifacts.json`, `metadata.json`, `config.yaml` and `CHANGELOG.md` when picking release assets.
- `Verify` refuses an `AllowUnsigned` Config (`ErrUnsigned`) and checks the signature against the public key file it is given, not `Config.TrustedKeys`; the fixture's TrustedKeys entry (`fixture.go`) exists only so `goreleaser.Render` emits `signs:`.

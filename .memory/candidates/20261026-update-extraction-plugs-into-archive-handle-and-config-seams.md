---
about: "Config.Extract reads the verified *Archive through ReaderAt/Size and owns closing it; format comes from AssetTemplate/AssetExt, the budget is Config.MaxExtractBytes, and the staged file lives in the caller's dir"
saw:
  - source/update/extract.go
  - source/update/archive.go
  - source/update/config.go
  - docs/adr/0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md
  - docs/adr/0021-extraction-derives-the-format-from-config-and-stages-the-binary-in-the-callers-directory.md
---

- `Config.Extract(ctx, *Archive, dir)` (extract.go) reads the archive through `io.NewSectionReader(a, 0, a.Size())`
  (zip: `zip.NewReader(a, a.Size())`) and closes it on success and on every failure after a non-nil archive
  (ADR-0020 "own closing it"). `Archive.Close` closes then removes (archive.go), and a close error after a
  successful extraction discards the staged binary and fails.
- The format is never sniffed: `deriveFormat` (config.go) maps the resolved `AssetTemplate` and `AssetExt`. A template
  without `{ext}` is raw; an empty `AssetExt` resolves to `tar.gz`, so it cannot mean raw. `Validate` rejects any
  other extension than `tar.gz`, `tgz` and `zip` when the template uses `{ext}`.
- The budget is `Config.MaxExtractBytes` (default 512 MiB, zero = default, negative invalid), enforced with the
  `readBound` limit+1 pattern; `*ExtractLimitError` is distinct from `*SizeLimitError`, which carries a URL.
- The staged file is `os.CreateTemp(dir, TempPrefix+"*")` in a directory the caller supplies, so a later rename
  onto the managed binary stays on one filesystem; `Config.TempDir` is for the download only. It is chmod 0700 and
  synced. `Staged.Keep` and `Staged.Discard` are separate because Windows cannot remove an open file.
- `resolve` rejects unknown extensions before `Extract` picks an extractor, so `*UnsupportedFormatError` is not
  reachable from a valid `Config`.

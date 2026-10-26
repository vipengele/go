---
about: "extraction (story #50) must read the verified *Archive via ReaderAt/Size, reuse Config budget style, and ADR/CONTEXT leave format, zip and output location undecided"
saw:
  - source/update/archive.go
  - source/update/download.go
  - source/update/config.go
  - source/update/AGENTS.md
  - source/update/.golangci.yml
  - docs/adr/0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - CONTEXT.md
---

Established while planning issue #50 (hardened extraction); `agtk memory search` found no note on it.

- Download hands off a verified `*Archive` (open 0600 temp file in `Config.TempDir`, rewound), exposing only
  Read/ReadAt/Seek/Size/Name/Close (archive.go; ADR-0020). ADR-0020 "Consequences" says extraction and
  replacement "read an `*Archive` ... through ReaderAt and Size, and own closing it". Not bytes, not a callback.
- Budget mechanism in download.go: `Config.MaxDownloadBytes` (default 256 MiB, config.go:27-28, negative invalid),
  announced size is only a hint, enforced while reading by `io.LimitReader(body, readBound(limit))` (limit+1,
  saturating at MaxInt64) then `written > limit` -> `*SizeLimitError{URL, Limit}` (errors.go:157). The checksums
  file has a fixed 1 MiB cap. Extraction has no Config budget field yet; `SizeLimitError` carries a URL, so reuse
  needs a decision.
- Format: `Config.AssetExt` (default `tar.gz`, asset.DefaultExt) renders `{ext}` into the asset name; config.go:59-61
  says a raw-binary tool leaves `{ext}` out of AssetTemplate. Select (select.go) does not tag the format, so
  extraction must be driven by config or sniffing; nothing records which.
- Seams: AGENTS.md Conventions: every outside-world access is a Config field resolved in `resolve`; new seam =
  new field with documented zero default. No Config seam for filesystem exists beyond TempDir/Executable.
- Lint: .golangci.yml enables gochecknoglobals (sentinel errors allowed) and depguard (no cli/core, cobra only in
  updatecmd). go.mod deps: cli/spec, commons, golang.org/x/mod only.
- No ADR/CONTEXT/note mentions zip, Windows archives, symlinks, hardlinks or decompression bombs; ADR-0003:22 only
  lists "archive extraction" and "atomic replacement of the managed binary" as update's job. CONTEXT.md has no
  Asset/Archive/Budget/Binary term; it has "Managed binary" (installed executable, replaced atomically with a
  verified staged binary) and "Release source". Nothing on lydite/gt extract is recorded in the repo.

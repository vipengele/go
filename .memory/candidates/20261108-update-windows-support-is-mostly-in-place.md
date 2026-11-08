---
about: "update already ships zip extraction, .exe binary lookup and the two-rename Install for Windows; the real gaps for issue #60 are a Windows runner (ADR-0019/0022 reject it), leftover .old cleanup on next start, and install.sh/installscript"
saw:
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - docs/adr/0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md
  - docs/adr/0021-extraction-derives-the-format-from-config-and-stages-the-binary-in-the-callers-directory.md
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
  - source/update/install.go
  - source/update/extract.go
  - source/update/asset/asset.go
  - source/update/AGENTS.md
  - .github/workflows/ci-build.yml
---

- ADR-0019 (file lock, "Considered options" and "Windows verification"): CI = `GOOS=windows go vet`
  cross-compile only; "A Windows CI runner. The cost ADR-0009 rejected, for every project." ADR-0009
  rejects "A Windows and macOS CI matrix" for cost. Neither forbids a runner outright; neither
  considers an opt-in/scheduled/per-project one. So criterion (c) needs a new ADR superseding/refining.
- ADR-0022 already decides: two renames (exe -> <exe>.old, staged -> exe), Windows one code path,
  NO `_windows.go` (would turn on the vet gate), `BackupLockedError` on errno 5/32 in Discard, `.old`
  orphan "invisible to the API, next Install overwrites it". No "cleanup on next start" design exists
  anywhere (grep of docs/adr, source/update): stale `.old` only goes on the next Install.
- ADR-0021 + extract.go:221-223: zip and `.exe` suffix are already done (extract_zip.go; AssetExt
  tar.gz|tgz|zip; `{ext}` placeholder in asset/asset.go). installscript is POSIX sh and does not
  handle zip/Windows (AGENTS.md installscript "Not handled").
- update is unreleased: no docs/release-notes/update, ADR-0009 says "update is unreleased", so
  defaults can change without migration; not in strict mode.
- update/.golangci.yml: gochecknoglobals; Config seams rule (AGENTS.md Conventions) means GOOS via
  Config, no runtime.GOOS.

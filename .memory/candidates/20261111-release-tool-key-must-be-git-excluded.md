---
about: "release-tool.yml writes the minisign key into the work tree, so it appends the key path to .git/info/exclude first: an untracked key makes GoReleaser refuse every real (non --snapshot) release as a dirty tree"
saw:
  - .github/workflows/release-tool.yml
  - source/update/goreleaser/render.go
  - docs/adr/0033-a-tool-releases-through-the-update-release-workflow-pinned-at-the-update-tag.md
---

- The rendered `signs:` block runs `minisign -S -s minisign.key` with the path relative to GoReleaser's working directory (`goreleaser.DefaultSigningKeyPath`, `source/update/goreleaser/render.go`), so the key has to sit inside the tool's checkout while GoReleaser runs.
- GoReleaser's git validation refuses a release from a dirty tree, and an untracked file counts. The `Exclude the key from the work tree` step in `.github/workflows/release-tool.yml` appends `/<repo-relative key path>` to `git rev-parse --git-path info/exclude` before the key is written; with the line, `goreleaser release --clean --skip=publish` succeeds, without it the run fails with "git is in a dirty state".
- `--snapshot` (or `--skip=validate`) skips that check, which is why the workflow runs on the real tag in both modes, the dry run included: a snapshot-based self-test would pass while every real release failed (ADR-0033).

---
about: "Constraints on the download/verify code in source/update: seams, single package, stdlib+x/mod, temp-file location decided in ADR-0020, lydite thresholds external"
saw:
  - source/update/AGENTS.md
  - source/update/config.go
  - source/update/errors.go
  - source/update/github.go
  - source/update/.golangci.yml
  - source/update/go.mod
  - docs/adr/0019-a-missing-asset-on-the-redirect-path-surfaces-at-download.md
  - docs/adr/0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - .lydite/components.yml
---

Re-verified against the code on 2026-10-26.

- Seams: every outside-world access is a Config field with a zero-value default resolved in Config.resolve (config.go). TempPrefix, TempDir, MaxDownloadBytes, DownloadTimeout, Executable, HTTPClient and Now are Config fields. The temp-file location and its rejected alternatives are decided in ADR-0020.
- gochecknoglobals + depguard (source/update/.golangci.yml): no mutable package vars, no cli/core import, cobra only in updatecmd. Single public package `update` plus `asset/`; no internal/. ADR-0003 places updatecmd as a separate package.
- Errors are typed in errors.go: StatusError, TimeoutError (net.Error, produced by classifyError), AssetNotFoundError, ChecksumMismatchError, ChecksumNotFoundError, ChecksumConflictError, SizeLimitError. ADR-0019: the download maps a 404 on a URL Select located (Selection.Located) to *AssetNotFoundError. GitHubSource.get (github.go) is the timeout pattern: context.WithTimeout + classifyError.
- Deps: stdlib + golang.org/x/mod (semver, go.mod) + siblings cli/spec and commons via directory replace (ADR-0008). Development mode: docs/release-notes holds only commons/.
- Coverage thresholds are not in the repo: .lydite/components.yml has `update` as plain go-test -race ./...; the gate is external reusable-lydite.yml@v2. Mutation exclusions: trailing `// [lydite:exclude_from_mutation][reason]`.

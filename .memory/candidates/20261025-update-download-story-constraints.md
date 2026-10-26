---
about: "Constraints for new download/verify code in source/update (#49): seams, single package, stdlib+x/mod, no ADR on temp-file location, lydite thresholds external"
saw:
  - source/update/AGENTS.md
  - source/update/config.go
  - source/update/errors.go
  - source/update/github.go
  - source/update/.golangci.yml
  - source/update/go.mod
  - docs/adr/0019-a-missing-asset-on-the-redirect-path-surfaces-at-download.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - .lydite/components.yml
---

Memory store held no note on update; this was read from code and ADRs on 2026-10-25.

- Seams: every outside-world access is a Config field with zero-value default resolved in Config.resolve (config.go:~120-190); Config.TempPrefix (default "<BinaryName>-update-") and Executable, HTTPClient, Now already exist. No fs seam exists yet; ADRs say nothing on temp-file location or rename-same-directory (grep of docs/adr for rename/same directory: nothing relevant).
- gochecknoglobals + depguard (source/update/.golangci.yml): no mutable package vars, no cli/core import, cobra only in updatecmd. Single public package `update` plus `asset/`; no internal/ yet. ADR-0003 places updatecmd as a separate package.
- Errors are typed in errors.go: StatusError, TimeoutError (net.Error, produced by classifyError), AssetNotFoundError. ADR-0019: download maps 404 on a URL Select located (Asset.Size==0, nil Assets) to *AssetNotFoundError. GitHubSource.get (github.go:311) is the timeout pattern: context.WithTimeout + classifyError; readLimited (github.go:428) is the existing size-cap helper.
- Deps: stdlib + golang.org/x/mod (semver, go.mod) + siblings cli/spec and commons via directory replace (ADR-0008). Development mode: docs/release-notes holds only commons/; `git tag` unavailable in this checkout so tags unchecked.
- Coverage thresholds are not in repo: .lydite/components.yml has `update` as plain go-test -race ./...; gate is external reusable-lydite.yml@v2 (ci-orchestration.yml:~160). Mutation exclusions: trailing `// [lydite:exclude_from_mutation][reason]`.

---
about: "constraints on an update test kit (issue #57): no ADR decides placement, logtest is the in-repo precedent for a public test-support subpackage, and the fake server must match GitHubSource's redirect/API shapes, checksum line format and per-request timeout"
saw:
  - source/update/AGENTS.md
  - source/update/github.go
  - source/update/download.go
  - source/update/config.go
  - source/update/.golangci.yml
  - source/cli/core/logging/logtest/recorder.go
  - source/cli/core/AGENTS.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0016-the-default-update-source-reads-the-github-latest-redirect-then-the-api.md
  - docs/adr/0019-a-missing-asset-on-the-redirect-path-surfaces-at-download.md
  - docs/adr/0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md
---

Explored 2026-10-26 for issue vipengele/go#57 (sub-issue of #8 "Self-update core").

- No ADR or CONTEXT.md entry covers test kits, test-only code in a published module, or a "testkit" name
  (`grep -rin "testkit\|test kit\|fake" CONTEXT.md docs/adr` -> only an unrelated hit in ADR-0012). ADR-0003
  lists update's contents without a test kit and puts cobra only in `updatecmd`.
- Precedent: `source/cli/core/logging/logtest` is a public subpackage of a project, documented in
  source/cli/core/AGENTS.md:151 and logging/doc.go:33. Subpackage `asset/` also exists in update.
  update has no internal/; its public subpackages are `asset/` and `updatetest/` (source/update/AGENTS.md
  Structure).
- Fake server must match: redirect path = GET `<WebURL>/<owner>/<repo>/releases/latest` answering 3xx with
  Location ending `/<owner>/<repo>/releases/tag/<tag>` (not followed); API path =
  `<APIURL>/repos/<owner>/<repo>/releases/latest` and `.../releases?per_page=100` (prereleases); assets are
  served at `<WebURL>/<owner>/<repo>/releases/download/<tag>/<name>` (GitHubSource.AssetURL, github.go:124).
  `GitHubSource.WebURL`, `APIURL`, `Timeout` are the test overrides (github.go:88-91), and `Config.Source` is
  pluggable (nil = GitHubSource, ADR-0016) via the `Source` interface plus optional `AssetLocator`.
- Checksums file: lines `<64 hex><spaces or *><name>` (sha256sum), LF/CRLF, either hex case, `./` prefix
  dropped, exact name match; max 1 MiB; duplicate name with differing digest = ChecksumConflictError
  (checksums.go:parseChecksum). Archive cap MaxDownloadBytes (256 MiB default). Digest decides, not
  Content-Length. Archive ext comes from Config.AssetExt; names from the asset template (`asset.Render`).
- Missing asset: on the redirect path a 404 on the located URL -> AssetNotFoundError (ADR-0019); on the API
  path an absent name in non-nil Assets is the miss in Select. A kit's "missing asset" must therefore be
  modelled per path. Download requests the checksums file BEFORE the archive.
- Slow download: DownloadTimeout is one deadline per request (headers+body), shorter caller ctx wins,
  both -> TimeoutError; see candidate 20261026-update-download-bound-and-timeout-test-pitfalls.md for the
  shared-deadline flake. A kit should block on a handler-side signal/context, not a short sleep.
- Dependency stance: update's go.mod is stdlib + x/mod + siblings (AGENTS.md; candidate
  20261025-update-download-story-constraints.md). The repo has no stated rule on `testing.TB` in non-test
  packages; logtest is precedent: logtest/assert.go:10,19 imports `testing` and takes `testing.TB` in a public package (recorder.go itself does not).
  depguard bans cli/core and cobra only. gochecknoglobals is on (no package-level vars).
- Status: update has no tag and no docs/release-notes/update/ (only commons/) -> development mode.
  Sub-issues of #8: #45-#49 closed; open #50 extraction, #51 replacement, #52 managed installs, #53 throttle,
  #54 state, #55 background check, #56 update command, #57 this kit. Release PR issues #93/#94 open.
  Nothing abandoned was found in ADRs, candidates or issue titles.

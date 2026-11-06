---
about: "constraints a release-channel plus downgrade-watermark change (issue 61) meets in source/update: State round-trips drop unknown keys, the redirect path cannot carry the prerelease flag, Apply holds no resolved Config or lock, and ADR-0024 left downgrade to this decision"
saw:
  - source/update/state.go
  - source/update/check.go
  - source/update/background.go
  - source/update/apply.go
  - source/update/github.go
  - source/update/config.go
  - source/update/updatecmd/updatecmd.go
  - docs/adr/0016-the-default-update-source-reads-the-github-latest-redirect-then-the-api.md
  - docs/adr/0024-release-assets-are-authenticated-by-a-minisign-signature-over-the-checksums-file.md
---

Read at origin/main on 2026-11-06 while planning issue 61. Nothing in the store or ADRs records
channels or a watermark except the two pointers below.

- ADR-0024 (Considered options, tumika row) rejects a signed manifest because "release channels are
  to be expressed through GitHub's pre-release flag and the tag"; its last Consequences bullet says
  downgrade/replay "is the release-channel decision's". ADR-0016 says the opposite of issue 61 on
  the flag: "The `prerelease` flag of the API response is not read, so both paths agree"
  (gitHubRelease, github.go:195-200, has no such field), so 61 amends ADR-0016.
- The redirect path (github.go:144-184) yields only tag + URL, no flag, no assets. A channel that is
  "higher of tag suffix and flag" cannot be decided on that path for Beta/Edge: `/releases/latest`
  never names a pre-release (ADR-0016), so any non-Stable channel must use the list route
  (highestListed, github.go:264-309), which today is the only path reading the list and which
  `updatetest` cannot express (no prerelease field in its release JSON, server.go:349 has Draft only).
- State is written whole: fileStore.write marshals the struct through cli/state.SaveJSON
  (state.go:133-145), and Load of a corrupt file is the zero State with nil error (state.go:124-131).
  Consequences: (a) a copy of the tool built before the new fields reads HighestApplied as an
  unknown key and its next afterAttempt Save writes the file without it, so the watermark is lost
  (the doc at state.go:18-19 only promises it stays readable); (b) a corrupt or deleted file
  silently resets the floor to the running Version, so the watermark fails open; only an
  unreadable file is an error.
- Who may save today: only runCheck (background.go:81-103), under Locker.Lock, after Load and a
  re-throttle. afterAttempt (check.go:96-105) copies st and edits LastAttempt/LatestVersion only,
  so new fields survive provided the State it is given was loaded under the lock. Start's
  unlocked Load (background.go:56) is never saved. A watermark write from Apply must therefore
  Load-modify-Save under the same Locker, not Save a State built elsewhere, or it clobbers
  LastAttempt/LatestVersion (and vice versa).
- Lock hazards for Apply: runCheck holds the lock across the Source query (up to Source timeout,
  10s default, github.go:27) and Finish's CheckWait bound (250ms default, max 2s, config.go:43-45)
  only covers this process. Another process's check can make Lock(ctx) wait; use ctx, or TryLock
  (dirLocker has one, config.go:234, which the check never uses), and treat failure like a failed
  save. `update` command carries spec.SkipUpdateCheck (updatecmd.go:48) so no check of the same
  process runs beside Apply.
- Apply never resolves the Config itself: c.apply calls CheckManaged/checkWritable/Download, each of
  which resolves (apply.go:35-60). Reading Locker/StateStore/Version in Apply needs c.resolve()
  (config.go ~480-514 sets Locker=dirLocker, StateStore=fileStore). Install returns *Installed with
  the backup kept (apply.go:59); the watermark should be written after Install returns, but before
  or after Discard is the caller's (updatecmd.go:204-217), so a failed save must not fail the run.
- Source implementers that a signature change Latest(ctx) -> Latest(ctx, ch) touches: background.go:94,
  updatecmd.go:135 and :167, download_test.go:1027, select_test.go:201, and the stubs
  background_test.go:89, config_test.go:27, example_test.go:187, updatecmd_test.go:683.
- updatecmd today: --to may downgrade and is the only way (AGENTS.md "updatecmd"; test
  TestToAnOlderReleaseDowngrades, updatecmd_test.go:559; releaseVersion accepts a prerelease).
  Order in run: parse flags, Resolved, CheckRelease, CheckManaged (--check tolerates a managed
  install), then report/update (updatecmd.go:74-112). Confirmation precedes Select/Apply
  (:179-204). Validation errors are plain (exit 1); `--check` found = spec.Coded(10, Reported).
  .golangci.yml denies cobra and cli/core in every file, tests included.
- Test timing: TestGitHubSource_Timeout (github_test.go:609) uses 50ms only where the stalled
  request is first; a request after an answering one needs 2s, else the deadline names the wrong
  URL. Any new list-route test with a preceding request must follow that. Server counts are read
  after the nested "lookup" subtest has shut the server (see the earlier candidates on request
  counts and wall-clock margins).
- Lydite: update component is `go-test -race ./...` (.lydite/components.yml:24-27); coverage and
  mutation thresholds live in the external reusable workflow, not this repo (see
  20261016-lydite-gates-for-first-code-project). The only in-repo mutation exclusion in update is
  config.go:309, syntax `// [lydite:exclude_from_mutation][reason]`; an ignored-save-failure
  branch would need one if a test cannot observe it.

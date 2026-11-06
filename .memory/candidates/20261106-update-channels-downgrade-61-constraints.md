---
about: "invariants of release channels and the downgrade watermark in source/update: who writes State and under which lock, why the guard fails closed and the record fails open, why a failed attempt records its channel, and why the redirect path serves Stable only"
saw:
  - source/update/state.go
  - source/update/check.go
  - source/update/background.go
  - source/update/apply.go
  - source/update/downgrade.go
  - source/update/github.go
  - source/update/updatecmd/updatecmd.go
  - docs/adr/0027-release-channels-come-from-the-tag-and-the-pre-release-flag-and-a-watermark-refuses-a-downgrade.md
---

Decisions are in ADR-0027; these are the mechanics a change to them has to keep.

- State is written whole. fileStore.write marshals the struct (state.go:160), and a missing or
  corrupt file loads as the zero State with a nil error (state.go:126). Two writers exist, and both
  Lock, Load, modify and Save under the same Locker: runCheck (background.go:88), through afterAttempt
  (check.go:146), and recordApplied (apply.go:123). A writer that saves a State built anywhere else
  clobbers the other writer's fields: LastAttempt, LatestVersion and LatestChannel on one side,
  HighestApplied on the other. A build of the tool that predates a key drops it on its next save.
- The guard fails closed and the record fails open. CheckDowngrade (downgrade.go:22) returns the
  Load error, so an unreadable state file refuses the install. recordApplied ignores lock, load and
  save failures, because the binary is already replaced and its Version floors the next run. A
  corrupt or deleted file resets the floor to Version. That is the documented limit, not a bug.
- The command guards twice. updatecmd calls CheckDowngrade before the confirmation prompt, so a
  refused downgrade never prompts, and Apply calls it again unless AllowDowngrade is given. --check
  maps a *DowngradeError to "up to date" (updatecmd.go, report). --to alone never lifts the guard.
- A failed attempt records its channel (afterAttempt). lastAttemptOn and latestVersionOn
  (state.go:40,49) read an attempt made on another channel as no attempt. If a failed attempt left
  the old channel recorded, an unreachable source would be queried on every run after a channel
  switch, with no throttle.
- The redirect path (latestByRedirect, github.go:147) yields only a tag and a URL, with no
  prerelease flag and no assets, and GitHub's /releases/latest never names a pre-release. Stable
  alone uses it. Beta and Edge use highestListed (github.go:276), which pages
  `?per_page=100&page=N` up to maxListPages (github.go:264). The updatetest kit has no list route,
  so list tests in github_test.go carry their own handlers.

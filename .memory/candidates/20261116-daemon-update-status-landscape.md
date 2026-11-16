---
about: "daemon has no dependency on update and no channel/watermark; ADR-0003 says it requires update but go.mod does not"
saw:
  - source/daemon/go.mod
  - source/daemon/health.go
  - source/daemon/health_http.go
  - source/daemon/daemoncmd/status.go
  - source/daemon/selfupdate/store.go
  - source/update/state.go
  - docs/adr/0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md
  - docs/adr/0027-release-channels-come-from-the-tag-and-the-pre-release-flag-and-a-watermark-refuses-a-downgrade.md
---
Found while planning #86 (update status in health and CLI).

- source/daemon/go.mod requires only cli/spec, cli/state, filelock, x/sys (commons indirect). No
  `go.vipengele.org/update`, though ADR-0003 and ADR-0033 say daemon requires update.
  `grep -rn go.vipengele.org/update source/daemon` -> 0 hits.
- Channel (`update.Config.Channel`, update/config.go:117) and watermark (`State.HighestApplied`,
  update/state.go:35, ADR-0027) exist only in source/update; daemon/selfupdate.Record has neither.
- Health /status wire contract (ADR-0025 "Wire contract"): added key keeps schema 1, rename/remove/retype
  bumps. Pinned by daemoncmd/testdata/status.keys via speccheck.AssertJSONKeys (status_test.go:225). Snapshot
  has fixed fields (health.go snapshotJSON); extension today is only via Add(Check), no extra payload.
- selfupdate.FileStore.Load takes no lock; writes go through cli/state atomic rename, so a separate
  process reading sees old or new record. Corrupt file is an error (state.ErrCorrupt), never idle. Path is
  caller-supplied (NewFileStore(path)); no default location.

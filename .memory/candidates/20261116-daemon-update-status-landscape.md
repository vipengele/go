---
about: "daemon has no dependency on update; channel and watermark reach it only as consumer-supplied strings"
saw:
  - source/daemon/go.mod
  - source/daemon/health.go
  - source/daemon/selfupdate/record.go
  - source/daemon/selfupdate/report.go
  - source/daemon/selfupdate/store.go
  - source/update/state.go
  - docs/adr/0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md
  - docs/adr/0036-daemon-update-status-is-reported-outside-the-check-model.md
---
- source/daemon/go.mod requires only cli/spec, cli/state, filelock, x/sys (commons indirect). No
  `go.vipengele.org/update`, though ADR-0003 and ADR-0033 say daemon may require update; ADR-0036
  records why it does not.
- Channel (`update.Config.Channel`) and watermark (`State.HighestApplied`, source/update/state.go,
  ADR-0027) live only in source/update; `selfupdate.Record` (selfupdate/record.go) has neither. The
  daemon carries them only as `selfupdate.ReleaseInfo` strings the consumer's `release` func returns
  to `selfupdate.Reporter` (selfupdate/report.go), copied into `daemon.UpdateStatus`.
- Health /status wire contract (ADR-0025): an added key keeps schema 1; rename/remove/retype bumps it.
  Pinned by daemoncmd/testdata/status.keys via speccheck.AssertJSONKeys. Beyond the checks, the one
  extra payload is the optional `update` object (`Snapshot.Update`, health.go), from
  `WithUpdateReporter`.
- selfupdate.FileStore.Load takes no lock; writes go through cli/state atomic rename, so a separate
  process reading sees the old or new record. A corrupt file is an error (state.ErrCorrupt), never
  idle. The path is caller-supplied (NewFileStore(path)); there is no default location.

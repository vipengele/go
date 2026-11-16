---
about: "update-status JSON splices daemon.UpdateStatus's own encoding, so its keys follow UpdateStatus"
saw:
  - source/daemon/daemoncmd/update_status.go
  - source/daemon/daemoncmd/testdata/update-status.keys
  - source/daemon/health.go
---
- `updateReport.MarshalJSON` (source/daemon/daemoncmd/update_status.go) writes `source`, then the inner
  bytes of `json.Marshal(daemon.UpdateStatus)` (outer braces stripped), then `daemon_error`. A key added
  to `updateStatusJSON` in health.go therefore appears in update-status's flat JSON as well as in
  /status, and both testdata/status.keys and testdata/update-status.keys need regenerating
  (`SPECCHECK_UPDATE=1`).
- `readUpdate` falls back to the `daemon.UpdateReporter` on any daemon failure except a done parent
  context, and on a reachable daemon whose snapshot has no `update`; `daemon_error` is set only for the
  failure case. With no fallback, a reachable daemon without `update` is `source: none`, exit 0.

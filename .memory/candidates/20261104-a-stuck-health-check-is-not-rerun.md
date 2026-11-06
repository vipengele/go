---
about: "A health check whose abandoned run has not returned is not started again: Snapshot reports it failed with ErrCheckTimeout without calling it"
saw:
  - source/daemon/health.go
  - source/daemon/health_test.go
---

- `Health.run` (health.go) abandons a check's goroutine when its timeout or the request
  context ends; the goroutine keeps running until the `Check` returns. Snapshots run on every
  `/livez`, `/readyz` and `/status` request with no cache, so re-running a check that ignores
  its context would leak a goroutine per probe.
- Each registered check carries a `*checkState` counting abandoned runs that have not returned.
  While the count is above zero, `run` fails the check at once with
  `ErrCheckTimeout: an earlier run is still running` and Duration 0, without calling it.
- Only an abandoned run blocks: concurrent snapshots whose runs finish in time still run a check
  concurrently. `checkState.idleChan` exists so tests can wait for an abandoned run to finish
  without sleeping (`TestSnapshotDoesNotRerunCheckWithAbandonedRun`).

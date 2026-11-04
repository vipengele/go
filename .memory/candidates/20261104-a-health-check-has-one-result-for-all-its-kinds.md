---
about: "A daemon health check has one pass/fail result for all its kinds, so a failing check that includes Liveness makes the snapshot down; the Supervisor registers supervisor.live and supervisor.ready separately for that reason"
saw:
  - source/daemon/health.go
  - source/daemon/supervisor.go
  - docs/adr/0023-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md
---

- `Health.snapshot` (health.go) marks the snapshot not live when any failing check has the
  `Liveness` kind, and not ready when any check fails. A single check carrying both kinds can
  therefore never report "live but unready": every failure is `down`.
- That is why `Supervisor.checks` (supervisor.go) registers two checks, `supervisor.live`
  (Liveness: from the start phase beginning until `Run` returns, isolated panics included) and
  `supervisor.ready` (Readiness). ADR 0023 records the single-check design as rejected.
- `/livez` (health_http.go) runs only checks that include `Liveness`; `/readyz` and `/status`
  run all of them, so a readiness dependency cannot delay or fail the restart probe.

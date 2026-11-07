---
about: "daemon's lifecycle test seams (stop/start/reload contexts, signal notify, waiting hook, health clock) are unexported fields; no exported test kit or lifecycle clock exists, and daemon has no release tag or notes yet"
saw:
  - source/daemon/supervisor.go
  - source/daemon/health.go
  - source/daemon/supervisor_test.go
  - source/daemon/http.go
  - source/daemon/export_test.go
---

Found while planning issue #74 (daemon test kit).

- Supervisor seams are unexported fields set by in-package tests: newStartContext, newStopContext,
  newReloadContext, waiting, notify/unnotify (supervisor.go:61-84, defaults at :140-146). Time in the
  lifecycle is only context.WithTimeout (grace, start, reload round); no time.Now/After/Sleep outside
  health.go. Health has an unexported clock (`now func() time.Time`, health.go:247, NewHealth passes
  time.Now at :340); no staleness concept, only per-check timeout (health.go:471, WithCheckTimeout).
- Shutdown is driven by the ctx given to Run; the package registers only SIGHUP (WithReload), never
  SIGINT/SIGTERM (doc.go:227; double-signal belongs to cli/core app.go:180).
- Fakes are duplicated in-package: fakeRunner, scriptedRunner, heldStart (supervisor_test.go),
  funcRun, httpRun, lateStart, fakeClock (health_test.go:16), harness (supervisor_test.go:429).
- `git tag -l` has no source/daemon/* tags and docs/release-notes has only commons/v0.1.0.md, so
  nothing in daemon is a released API yet. No ADR covers test-helper packages or clock injection
  (grep clock|xxxtest|testkit in docs/adr -> 0 hits). No tumika notes in .memory.

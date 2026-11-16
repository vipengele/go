---
about: "Constraints on placing a podman+systemd supervisor integration harness (vipengele/go#82): service.New refuses in a container unless WithContainer(false), RestartSec=5, no Restart on Manager, no integration precedent"
saw:
  - source/daemon/service/manager.go
  - source/daemon/service/systemd.go
  - source/daemon/service/systemd_unit.go
  - source/daemon/service/state.go
  - source/daemon/service/systemd_status.go
  - docs/adr/0028-lifecycle-time-and-readiness-are-injectable-seams.md
  - docs/adr/0029-the-container-runtime-supervises-a-containerised-daemon.md
  - docs/adr/0030-windows-tests-run-for-projects-with-windows-files.md
  - .github/workflows/ci-test.yml
  - .lydite/components.yml
  - source/daemon/AGENTS.md
---

Explored 2026-11-16 planning #82. No note covered it.

- service.New refuses inside a container (manager.go, check precedes Config validation and GOOS
  switch; ADR-0029). `WithContainer(func)` or `<PREFIX>_CONTAINER=0` (with WithIdentity) forces it
  off; WithContainer wins. A podman-systemd harness must use one of these to reach the real driver.
- systemd driver runs only `systemctl`/`launchctl`/`id` via execRunner allowlist (systemd.go:26);
  runner is a struct field set in New, not a public option. System scope needs euid 0
  (requirePrivileges, systemd.go:137); user scope needs a user manager (ErrManagerUnreachable).
- Unit always writes Restart=always, RestartSec=5 (systemd_unit.go:113-114): crash-loop and
  kill -9 restart tests wait >=5s per restart unless the harness shortens it.
- Status has State/PID/Restarts/Detail; StateFailed = failed or restarted while starting
  (state.go resolve; systemd_status.go reads NRestarts, Result). Crash-loop reporting exists.
- Manager has no Restart method (Prepare/Install/Uninstall/Start/Stop/Status). #80 (service
  commands in daemoncmd, ADR-0003) does not gate driving Manager directly; daemoncmd has only status.
- Precedent for a public test kit: daemon/daemontest (ADR-0028), depguard restricts importers to
  daemoncmd (AGENTS.md:335-337). ci-test.yml has no podman/build-tag/integration job; the only
  go:build tags in daemon are !windows / windows. No prior integration harness or ADR (grep
  podman|e2e|integration|harness over docs, agentic, .github, .lydite).
- ci-test.yml runs plain `go test -race -shuffle=on ./...` on ubuntu-latest and windows-latest; a
  build tag keeps a test out of both and out of lydite (components.yml args `-race ./...`).
  Podman presence on ubuntu-latest and systemd-in-podman there: UNVERIFIED.

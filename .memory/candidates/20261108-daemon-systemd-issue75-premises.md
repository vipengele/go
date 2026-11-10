---
about: "daemon has no ServeListener; the socket-activation seam is NewHTTPServerListener, and the systemd driver renders Type=simple unless WithSystemdNotify asks for Type=notify"
saw:
  - source/daemon/http.go
  - source/daemon/service/manager.go
  - source/daemon/service/systemd_unit.go
  - source/daemon/service/option.go
  - source/daemon/supervisor.go
---

Issue #75 (systemd readiness / socket activation) was written against another codebase; its premises differ from this one:

- No `ServeListener` symbol exists. The seam for an inherited socket is `NewHTTPServerListener(srv, l)` (source/daemon/http.go), which takes ownership of `l` and closes it when the server stops; a socket-activated listener from `systemd.Listeners` is passed there.
- `service.New` selects the systemd driver on linux and returns `ErrUnsupported` elsewhere (source/daemon/service/manager.go). The unit is rendered by `renderUnit(cfg, h, n)` (source/daemon/service/systemd_unit.go): `Type=simple` by default, `Type=notify` plus `WatchdogSec=<ms>ms` only with `WithSystemdNotify` (source/daemon/service/option.go). It is opt-in because a Type=notify unit whose binary never sends READY=1 fails its start.
- READY=1 is sent right after the `WithReady` callback in `wait`, STOPPING=1 right after `setPhase(phaseStopping)` in `Run` (source/daemon/supervisor.go).

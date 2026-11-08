---
about: "systemd Status passes restarts=0 to resolve when Result=success, because restart.go turns a requested restart into exit 0 and Restart=always then restarts it"
saw:
  - source/daemon/service/systemd_status.go
  - source/daemon/service/state.go
  - source/daemon/restart.go
  - source/daemon/service/systemd_unit.go
---

`resolve(observed, restarts)` (source/daemon/service/state.go) reads Starting with restarts > 0 as
Failed. A daemon that asks for a restart, or applies an update, exits 0 (source/daemon/restart.go),
and the unit's fixed `Restart=always` / `RestartSec=5` (systemd_unit.go) brings it back, with
systemd's `NRestarts` counting up. Without a correction, every update reads as Failed for the
restart window.

`(*systemd).show` (systemd_status.go) therefore passes restarts=0 when `Result=success`, while
`Status.Restarts` still reports the real NRestarts. A crash loop (Result other than success) keeps
its count and reads Failed. `resolve` itself is shared with the other drivers and stays unchanged.
A missing `NRestarts` (systemd before 235) counts as 0.

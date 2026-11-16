---
about: "the health UpdateReporter runs through the same bounded helper as checks and never on the probe path"
saw:
  - source/daemon/health.go
  - source/daemon/health_http.go
  - source/daemon/supervisor.go
---
- `bounded[T]` (source/daemon/health.go) holds the deadline, panic recovery, abandon and stuck logic
  for both `Health.run` (checks) and `Health.report` (the UpdateReporter). A change to how a check is
  bounded changes the reporter too; the reporter has its own `checkState` (`reporterState`), so a stuck
  reporter is never called again until it returns, and the snapshot reports `unknown` meanwhile.
- `Health.snapshot(ctx, only)` is the probe path and never calls the reporter; `Health.Snapshot` (and
  `/status`) go through `collect(ctx, 0, true)`. health_http.go picks `h.snapshot` for /livez and
  /readyz and `h.Snapshot` for /status.
- supervisor.go's systemd watchdog calls `s.health.snapshot(context.Background(), Liveness)`; routing
  it through `Snapshot` would put a slow self-update store on the watchdog's path.
- `UpdateState.known()` maps an empty or unrecognised state to `unknown` on encode, decode and from the
  reporter, so a state added later reads as `unknown` in older clients, never as `idle` (ADR-0033).

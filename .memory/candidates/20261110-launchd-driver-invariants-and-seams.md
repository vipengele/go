---
about: "What a launchd driver must honour from the systemd driver, and the New/test seams a darwin case flips"
saw:
  - source/daemon/service/manager.go
  - source/daemon/service/state.go
  - source/daemon/service/systemd.go
  - source/daemon/service/systemd_status.go
  - source/daemon/service/config.go
  - source/daemon/service/manager_test.go
  - source/daemon/service/systemd_unit_test.go
  - docs/adr/0024-service-drivers-are-selected-at-runtime.md
  - docs/adr/0028-driver-specific-settings-are-options-not-config-fields.md
---

Derived 2026-11-10 while planning a launchd driver (no store note matched).

- `New` has `case "darwin"` returning ErrUnsupported (manager.go:90-91). Tests that pin that:
  manager_test.go:30-31 (TestNewUnsupported lists darwin) and :353 (TestNewWithContainerFalseLeavesResults).
  Adding the driver must move darwin out of both, or they fail.
- Status semantics live in `resolve` (state.go:105-109): driver reports activating as StateStarting; Starting
  with restarts>0 becomes Failed. systemd_status.go:90-97 zeroes restarts when Result=success, so a
  restart after a clean exit (ErrRestartRequired -> exit 0, restart.go) stays Starting. A launchd driver
  needs its own "last exit was clean" signal (launchctl print: `last exit code`, `runs`) to keep this.
- Install = daemon-reload, enable, restart (systemd.go:217-241, never `enable --now`) so Install also applies an update.
- Config is POSIX-validated for darwin (config.go:79-83,163-178); ScopeUser with User set is rejected on every non-Windows GOOS (config.go:175).
- Hardening/notify options are ignored by other drivers but validated in New on every GOOS (manager.go:76-82; ADR-0028).
- Golden pattern: checkGolden + `-update` flag registered in TestMain (systemd_unit_test.go:12-35, main_test.go); goldens in testdata/systemd/. No darwin CI runner is needed because ADR-0024 requires every driver to compile and test on Linux (plain Go rendering, injected `run` runner as systemd.go:18,53).
- Only ADR-0019 style cross-vet applies to `_windows.go` files; no equivalent for darwin exists, and none is needed if there is no `_darwin.go`.

---
about: "What the launchd driver honours from the systemd driver, and the New and test seams it uses"
saw:
  - source/daemon/service/manager.go
  - source/daemon/service/state.go
  - source/daemon/service/launchd.go
  - source/daemon/service/launchd_status.go
  - source/daemon/service/systemd.go
  - source/daemon/service/config.go
  - docs/adr/0024-service-drivers-are-selected-at-runtime.md
  - docs/adr/0028-driver-specific-settings-are-options-not-config-fields.md
---

- `New` builds the launchd driver in `case "darwin"` (manager.go:96) with the real seams: `execRunner`,
  `os.Geteuid`, `os.Getuid`, `os.LookupEnv` and `sleepContext` (manager.go:115). Only windows and every
  other GOOS but linux and darwin return ErrUnsupported.
- Status semantics live in `resolve` (state.go:104): a driver reports a relaunching job as StateStarting, and
  Starting with restarts>0 becomes Failed. The launchd driver keeps systemd's "a restart after a clean exit is
  not a failure" rule by counting `runs - 1` as restarts only when the last run failed
  (launchd_status.go:104, `lastRunFailed` at launchd_status.go:126), the counterpart of
  systemd_status.go zeroing restarts on Result=success.
- launchd's Install (launchd.go:291) is the counterpart of systemd's enable-then-restart: enable, bootout,
  wait until unloaded (`waitUnloaded`, launchd.go:230), bootstrap; that is what makes a changed plist or binary
  take effect.
- Config is POSIX-validated for darwin (config.go); ScopeUser with User set is rejected on every non-Windows GOOS.
- Hardening, notify and launchd-log options are validated in New on every GOOS and ignored by the other drivers
  (ADR-0028).
- No `_darwin.go` file exists: both drivers compile and are tested on Linux through injected struct fields
  (ADR-0024), so CI needs no macOS runner and has none.

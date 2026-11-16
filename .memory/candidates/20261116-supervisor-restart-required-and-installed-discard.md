---
about: "Runner exit with ErrRestartRequired vs ErrRunnerExited, and Installed.Discard would delete the backup Machine rollback needs"
saw:
  - source/daemon/supervisor.go
  - source/daemon/restart.go
  - source/daemon/errors.go
  - source/update/install.go
  - source/update/stale.go
  - source/daemon/selfupdate/machine.go
---

- A Runner that exits before shutdown ends Run: nil Err gives `ErrRunnerExited` (supervisor.go:587), a non-nil Err is returned wrapped as `daemon: runner %q: ...` via l.wrap (supervisor.go:589). So a Runner whose Err wraps ErrRestartRequired is NOT ErrRunnerExited; it is the failure that begins shutdown, the other Runners are stopped in reverse order, and Run returns it joined with any stop problems. `RestartRequested` (restart.go:20) is true only when every leaf is ErrRestartRequired and no *PanicError, so an ErrStopTimeout joined in makes it false. Unwrap must be preserved (use %w).
- `Installed.Discard` (install.go:199) removes `<target>.old` unconditionally. Machine.Confirm and rollback depend on that same file (machine.go Confirm removes Backup; restore renames it). So after Begin, do NOT Discard; leave Installed unsettled and let the next boot's Confirm remove it or Boot roll it back. Rolling back the same update from the installing process uses Installed.Rollback, which does not touch the record.
- `BackupGuard` (config.go:207) only gates `RemoveStaleBackup` (stale.go:51), and the check hook calls that only on Windows (checkhooks.go, removeStaleBackupOnHost). It does NOT gate Installed.Discard. Wire `*Machine` as `Config.BackupGuard` (ADR-0033 Consequences).

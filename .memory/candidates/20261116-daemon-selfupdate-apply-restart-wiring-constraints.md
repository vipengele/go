---
about: "daemon self-update apply/restart (#85): restart contract is Restart=always/KeepAlive=true/SCM nonzero code; daemon does not yet require update; no helper compares current exe to service Binary"
saw:
  - source/daemon/service/systemd_unit.go
  - source/daemon/service/launchd_plist.go
  - source/daemon/service/windows.go
  - source/daemon/restart.go
  - source/daemon/selfupdate/machine.go
  - source/daemon/daemoncmd/service.go
  - source/daemon/go.mod
  - source/update/apply.go
  - source/update/install.go
  - source/update/managed.go
  - docs/adr/0033-a-daemon-self-update-restores-the-binary-before-recording-the-rollback.md
  - docs/adr/0034-a-requested-restart-on-windows-is-a-non-zero-service-exit.md
---
Found while scoping #85 (apply, exit, supervised restart). Read directly, no memory note existed.

- Exit 0 relaunches: systemd_unit.go:113-114 `Restart=always`, `RestartSec=5`; launchd_plist.go:57-59 plain `KeepAlive <true/>` (comment says self-update exit 0 must relaunch). Windows: SCM does not relaunch exit 0; ADR-0034 uses a non-zero restart code plus SetRecoveryActionsOnNonCrashFailures (windows.go:500-508). Hand-installed services lack this. Container: ADR-0029 needs a restart policy that restarts on exit 0, not on-failure.
- Pending record: selfupdate.Machine.Begin(Pending{ToVersion,Target,Backup}) (machine.go:92) needs Backup = update.Installed.Backup(), which exists only after Config.Apply/Install, so Begin happens AFTER the rename, not before (ADR-0033 recovers from a missing/stale record: Boot with pending ToVersion != running records rolled_back). Boot() (machine.go:139) counts attempts at start; Confirm() (machine.go:203) after the new build proves itself. Apply returns *Installed unsettled (apply.go:72 doc): caller must NOT Discard/Rollback it when the Machine owns the backup, else the backup Boot needs is gone. Machine implements update.Config.BackupGuard (ADR-0033).
- source/daemon/go.mod does NOT require go.vipengele.org/update, yet ADR-0003/0033 say daemon requires update. #85 must add `require go.vipengele.org/update v0.0.0` plus `replace => ../update` (ADR-0006).
- No existing helper tells whether the running executable is the service's Binary (service.Config.Binary, config.go:39) and no supervisor-detection helper; only NOTIFY_SOCKET in option.go and hostIsWindowsService (windows_service_*.go). Manager.Restart (systemd.go:319) starts a stopped unit, so "leave stopped alone" needs a Status()==StateRunning check first (state.go:10-25 States: NotInstalled/Stopped/Starting/Running/Failed).
- Windows: running exe can be renamed but not removed; <exe>.old lingers (ADR-0031); update.RemoveStaleBackup is guarded by Machine.BackupInUse. daemon already has *_windows.go, so GOOS=windows vet and the windows-latest test job apply (ADR-0030).

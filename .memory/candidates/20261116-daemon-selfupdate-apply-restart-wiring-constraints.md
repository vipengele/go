---
about: "daemon self-update apply/restart: restart contract is Restart=always/KeepAlive=true/SCM non-zero code; daemon requires update by a directory replace; the record is written only after the rename"
saw:
  - source/daemon/service/systemd_unit.go
  - source/daemon/service/launchd_plist.go
  - source/daemon/service/windows.go
  - source/daemon/restart.go
  - source/daemon/selfupdate/machine.go
  - source/daemon/selfupdate/apply.go
  - source/daemon/daemoncmd/update.go
  - source/daemon/supervised.go
  - source/daemon/go.mod
  - source/update/apply.go
  - source/update/install.go
  - docs/adr/0033-a-daemon-self-update-restores-the-binary-before-recording-the-rollback.md
  - docs/adr/0034-a-requested-restart-on-windows-is-a-non-zero-service-exit.md
---
- Exit 0 relaunches: systemd_unit.go `Restart=always`, `RestartSec=5`; launchd_plist.go plain
  `KeepAlive <true/>`. Windows: the SCM does not relaunch exit 0; ADR-0034 uses a non-zero restart
  code plus SetRecoveryActionsOnNonCrashFailures (windows.go). Hand-installed services lack this.
  Container: ADR-0029 needs a restart policy that restarts on exit 0, not on-failure.
- `selfupdate.Machine.Begin(Pending{FromVersion, ToVersion, Target, Backup})` needs
  Backup = `update.Installed.Backup()`, which exists only after `Config.Apply`/`Install`, so the
  record is written AFTER the rename. A crash between the rename and Begin leaves an installed
  binary no record covers; closing it needs an `update` API that names the backup before Install.
- `Machine.Apply` (selfupdate/apply.go) records with Begin before running the post-update hooks;
  the `update` command path (`daemoncmd.NewUpdate` through `updatecmd.WithInstalled`) runs the
  hooks first, because updatecmd calls the hook after `RunPostUpdate`.
- source/daemon/go.mod requires `go.vipengele.org/update v0.0.0` with `replace => ../update`
  (ADR-0006), and inherits update's `golang.org/x/crypto` pseudo-version as an indirect require.
- `daemon.UnderSupervisor` (supervised.go) is the supervisor-detection helper; `daemoncmd`'s
  update command compares the installed path with `UpdateConfig.ServiceBinary` (EvalSymlinks + Abs)
  before restarting. `Manager.Restart` starts a stopped unit, so the command reads Status first and
  restarts only a Running service.

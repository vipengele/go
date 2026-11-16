---
about: "the Windows service driver reaches the SCM only through the unexported scm seam, and a requested restart on Windows is a non-zero service exit that the SCM recovery action relaunches"
saw:
  - source/daemon/service/windows.go
  - source/daemon/service/scm_windows.go
  - source/daemon/service/scm_other.go
  - source/daemon/service/manager.go
  - source/daemon/windows_service.go
  - docs/adr/0034-a-requested-restart-on-windows-is-a-non-zero-service-exit.md
---

- `service.New`'s `case "windows"` returns a `windowsDriver` whose `scm` field is the host's
  (`hostSCM()`, manager.go). The driver (windows.go) is plain Go on every host; only
  `scm_windows.go` imports golang.org/x/sys, and `scm_other.go` (`!windows`) fails every call with
  `ErrUnsupported`, so `New(…, WithGOOS("windows"))` succeeds off Windows and each method fails.
- The SCM is not reached through `execRunner` (systemd.go): its allowlist stays systemctl,
  launchctl and id, and the Windows driver never runs sc.exe.
- systemd (`Restart=always`) and launchd (`KeepAlive`) relaunch a requested restart's exit 0; the
  SCM does not. `Install` sets one recovery action, a restart after 5s, plus the
  failure-actions-on-non-crash-failures flag (windows.go `configure`), and a Supervisor under
  `WithWindowsService` stops with `daemon.WindowsRestartExitCode` for a requested restart
  (windows_service.go `windowsExitCode`). A Stop or Shutdown from the SCM exits 0 whatever Run
  returned, so an operator's stop is never relaunched (ADR-0034).
- The driver's `Status` reads a STOPPED service with service-specific exit code
  `daemon.WindowsRestartExitCode` as `starting` (windows.go `stopped`), so it keeps reading
  `starting` after a Stop issued inside the restart delay until the service next starts.

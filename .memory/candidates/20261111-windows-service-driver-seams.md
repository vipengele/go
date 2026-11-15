---
about: "the service Manager has no Windows driver; a Windows driver must break two ADR-0024 conventions (exec runner, no _windows.go) and restart-as-exit-0 does not map to SCM recovery"
saw:
  - source/daemon/service/manager.go
  - source/daemon/service/systemd.go
  - source/daemon/restart.go
  - source/daemon/supervisor.go
  - docs/adr/0024-service-drivers-are-selected-at-runtime.md
  - docs/adr/0030-windows-tests-run-for-projects-with-windows-files.md
---

Found while planning issue #79 (Windows service driver). No note covered this.

- `service.New` has `case "windows": return nil, ErrUnsupported` (manager.go:106), documented at manager.go:49 and
  service/doc.go:8. A new driver is one `case` (ADR-0024 Consequences).
- Every current driver reaches the OS through the `runner` seam, whose `execRunner` is an allowlist of
  systemctl/launchctl/id (systemd.go:19-45). SCM via x/sys/windows/svc/mgr is not exec: it needs a new seam, and
  ADR-0024 says only the SCM calls may live in `_windows.go`, which turns on ADR-0030's windows-latest test job and
  `GOOS=windows go vet`. Keep rendering/validation (isAbs, validWindowsAccount in config.go) host-independent.
- Restart contract: `RestartRequested` => consumer exits 0 (restart.go:9-16, ADR-0021). systemd is Restart=always
  (systemd_unit.go), launchd KeepAlive plain true (launchd_plist.go:57-59) precisely so exit 0 relaunches. SCM failure
  actions fire only on a non-zero exit when the failure-actions-on-non-crash flag is set, so a Windows driver must set
  that flag (external SCM knowledge, not verified in-repo) or exit 0 will not restart.
- No "run under the supervisor" entry point exists: Supervisor.Run (supervisor.go:235) only takes a ctx; the package
  never subscribes SIGINT/SIGTERM (doc.go "Signals"). A svc.Handler must cancel that ctx on svc.Stop/Shutdown.
- x/sys is only an indirect dep of daemon (go.mod:12); no *_windows.go exists in source/daemon. Only filelock
  (lock_windows.go) holds a non-test one.

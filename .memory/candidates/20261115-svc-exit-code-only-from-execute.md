---
about: "under golang.org/x/sys/windows/svc a service-specific exit code reaches the SCM only through Execute's return value; a Stopped status the handler sends reports exit 0"
saw:
  - source/daemon/windows_service_windows.go
  - source/daemon/windows_service.go
---

- `svcHandler.Execute` (windows_service_windows.go) returns `(code != 0, code)` and never sends a
  Stopped status itself: x/sys svc sends every status the handler reports with the exit code still
  0, then sends Stopped with Execute's returned code once Execute returns, so a handler that sent
  Stopped would report a clean stop and the SCM would skip the recovery restart.
- A returned code of 0 is reported as NO_ERROR whatever the svcSpecificEC flag says; a non-zero
  code with the flag set is ERROR_SERVICE_SPECIFIC_ERROR (1066) carrying the code, which is what
  the service driver's `Status` reads back (source/daemon/service/windows.go `stopped`).
- `svc.Run` keeps a package global, so one process serves one service, and the dispatcher can be
  started once: a second `Supervisor.Run` returns `ErrAlreadyRan` before reaching the SCM
  (supervisor.go `Run`).

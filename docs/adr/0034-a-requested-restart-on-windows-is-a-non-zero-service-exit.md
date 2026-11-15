# On Windows a requested restart is a non-zero service exit

Status: accepted. It extends ADR-0021 (`0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md`)
and ADR-0029 (`0029-the-container-runtime-supervises-a-containerised-daemon.md`), whose requested
restart is an exit status 0 that the service manager answers with a relaunch, and it adds the
Windows driver to the drivers of ADR-0024 (`0024-service-drivers-are-selected-at-runtime.md`). It
does not amend ADR-0024. The Windows tests that cover the driver are those of ADR-0030
(`0030-windows-tests-run-for-projects-with-windows-files.md`), which supersedes ADR-0024's line
that CI never runs Windows.

On systemd (`Restart=always`) and launchd (`KeepAlive=true`) a Runner that returns
`ErrRestartRequired` ends the process with exit status 0, and the service manager starts it again.
The Windows service control manager (SCM) does not start a service that stops cleanly, so the same
exit leaves the service stopped. `go.vipengele.org/daemon/service` fronts the SCM through
`golang.org/x/sys/windows/svc` and `svc/mgr`, and the Supervisor option `WithWindowsService()` runs
the daemon as a service. A requested restart reaches the SCM as a failure it recovers from.

## The rules

- **Install sets one recovery action.** `Install` sets exactly one SCM recovery action, a restart
  after 5 seconds, with a reset period. 5 seconds is systemd's `RestartSec` and launchd's
  `ThrottleInterval`. `Install` also sets the failure-actions-on-non-crash-failures flag
  (`SetRecoveryActionsOnNonCrashFailures`), without which the SCM runs a recovery action only for a
  process that dies without reporting `SERVICE_STOPPED`.
- **A restart is a reported, non-zero stop.** The handler reports `SERVICE_STOPPED` with a non-zero
  service-specific exit code, the restart code. The SCM treats it as a failure and runs the
  recovery restart. A service-specific exit code is delivered only through the `(true, code)`
  return value of `Execute` in `x/sys/windows/svc`; a `Stopped` status sent by the handler reports
  exit 0.
- **A stop the SCM asked for exits 0.** When the SCM sent the Stop or Shutdown control, the exit
  code is 0 whatever `Run` returns, so a service an operator stopped is never restarted.
- **Any other ending is non-zero.** Otherwise a requested restart reports the restart code, and
  every other ending, a nil error included, reports a distinct failure code. This is the parity of
  systemd's `Restart=always`, which relaunches on any exit.
- **The codes are constants.** The restart code and the failure code are exported constants of the
  `daemon` package, so the handler, `Status` and a test name the same values.
- **Status reads the exit code.** The service `Status` maps `STOPPED` with the restart code to
  `starting`, because the service is inside the 5-second restart delay. `STOPPED` with any other
  non-zero code maps to `failed`.

## Considered options

- **The handler restarts itself through `svc/mgr`.** The process opens its own service and starts
  it again, or spawns a helper that does. It needs the service account to hold `SERVICE_START`
  rights on its own service, it races with the exit of the process that issues it, and it puts the
  restart logic in the process, where systemd and launchd keep it in the service manager.
- **Crash-only recovery with a faked crash.** The process exits abnormally without reporting
  `STOPPED`, so the SCM sees a crash. The SCM logs an unexpected termination, the graceful status
  reporting is skipped, and a real crash can no longer be told from a requested restart. The
  non-crash-failures flag lets a clean, reported, non-zero exit trigger the same recovery.

## Consequences

- **A requested restart relaunches the service on Windows as on the other systems,** with the SCM
  doing the relaunch and the process holding no restart logic.
- **A failed restart-requested exit is visible to the operator.** The SCM records a non-zero exit
  for it, where systemd and launchd record exit 0 (ADR-0021).
- **A Stop during the restart delay is an open risk.** A Stop issued in the 5 seconds after the
  restart code is reported finds the service already `STOPPED`, and the queued recovery restart may
  still fire. The end-to-end test asserts that the service stays stopped.
- **The recovery policy lives in the installed service.** A service installed by hand without the
  recovery action and the flag stops on a requested restart and is not relaunched.

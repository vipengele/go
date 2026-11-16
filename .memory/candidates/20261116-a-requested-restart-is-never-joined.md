---
about: "daemon.RestartRequested is false once any leaf of a joined error is not a restart, so the update guard and Machine.Apply log their side errors instead of joining them"
saw:
  - source/daemon/restart.go
  - source/daemon/update_guard.go
  - source/daemon/selfupdate/apply.go
---
- `RestartRequested` (restart.go) reports a restart only when every leaf of the error, through
  `onlyRestarts`, is or wraps `ErrRestartRequired` (and no `*PanicError` is in it); an `errors.Join`
  with any other error is a failure, so the process would exit non-zero and a supervised restart
  would read as a crash.
- `bootUpdate` (update_guard.go) therefore logs the store error of a Boot that returns
  `(true, err)` at Warn and returns a bare wrap of `ErrRestartRequired`.
- `Machine.Apply` (selfupdate/apply.go) logs a failed post-update hook and the not-supervised
  warning instead of returning them, and its result wraps `ErrRestartRequired` and nothing else.

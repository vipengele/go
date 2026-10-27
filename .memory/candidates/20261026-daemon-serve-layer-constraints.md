---
about: "Constraints the daemon runner layer keeps from ADR-0014/0010/0012: signals and exit codes stay outside, go.mod stays stdlib-only, and a restart is a clean exit through RestartRequested"
saw:
  - source/daemon/supervisor.go
  - source/daemon/restart.go
  - source/daemon/http.go
  - source/daemon/go.mod
  - source/daemon/.golangci.yml
  - docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md
  - docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md
  - docs/adr/0012-signals-are-registered-by-execute-and-injected-through-env.md
---

- `source/daemon/go.mod` has no require; depguard in `source/daemon/.golangci.yml` denies cobra and `go.vipengele.org/cli/core/**`. So the restart mapping cannot call cli/core: `RestartRequested(err)` (restart.go) only reports, and the consumer returns nil from its run function so the process exits 0 and `Restart=always`/`KeepAlive` relaunch it without a journal failure (ADR-0021; exit codes themselves are ADR-0010's).
- `RestartRequested` is true only when every leaf of a joined error is or wraps `ErrRestartRequired`, and any `*PanicError` in the tree makes it false. It walks `Unwrap() []error` / `Unwrap() error` itself: `errors.Is` on a wrapper node is true for a wrapped join that also holds other errors.
- Accepted cost recorded in ADR-0021: a non-Canceled error from a Runner exiting during shutdown makes `RestartRequested` false and the process exit non-zero.
- Signals stay the caller's (ADR-0012): the daemon registers none; the context passed to `Supervisor.Run` drives shutdown. The HTTP runner (http.go) gives handlers the grace period to drain by cancelling its `BaseContext` only after `Shutdown` ends, rather than cancelling in-flight requests when shutdown begins.

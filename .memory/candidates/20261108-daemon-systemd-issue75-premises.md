---
about: "daemon has no ServeListener, no unit rendering and no socket-file handling; issue #75 premises (Type=simple, ServeListener) do not match the code"
saw:
  - source/daemon/http.go
  - source/daemon/service/manager.go
  - source/daemon/supervisor.go
  - source/daemon/option.go
  - docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md
  - docs/adr/0024-service-drivers-are-selected-at-runtime.md
---

Checked 2026-11-08 for planning issue #75 (systemd readiness / socket activation).

- No `ServeListener` symbol: `grep -rn ServeListener` -> 0 hits. The seam is `NewHTTPServerListener(srv, l)` (http.go:98), which takes ownership and closes the listener when the server stops (http.go:90); it never touches a socket path.
- `service.New` returns `ErrUnsupported` for linux/darwin/windows (service/manager.go:41-47): no drivers, no unit text, so "installs as Type=simple" is not in code. grep Type=/NOTIFY_SOCKET/LISTEN_FDS across repo -> 0 hits. ADR-0024 says drivers are plain Go on all OSes, no build tags; ADR-0021 and ADR-0023 list sd_notify/socket activation as out of scope.
- Readiness hook exists: `WithReady` (option.go:82), called once in `wait` (supervisor.go:414) after all Starts return, synchronously on Run's goroutine (must not block). NewHTTPServer binds in Start, so ready follows bind.
- No os.Getenv in non-test daemon code; only test LookupEnv (instance_process_test.go:37). Instance lock is a filelock in the runtime dir, not a socket probe (ADR-0023).
- daemon has no `docs/release-notes/daemon/` and no `source/daemon/v*` tag (git ls-remote: none), so not strict mode and unreleased.

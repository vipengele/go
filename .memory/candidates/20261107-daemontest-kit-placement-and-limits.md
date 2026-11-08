---
about: "daemontest tests must be internal because depguard lets only daemoncmd import daemontest, and daemontest.Start cannot return while a runner's Start hangs"
saw:
  - source/daemon/.golangci.yml
  - source/daemon/daemontest/daemon.go
  - source/daemon/daemontest/runner.go
  - source/daemon/daemontest/clock.go
---

- depguard rule `root-imports-only-the-instance-lock-siblings` in source/daemon/.golangci.yml
  allows `go.vipengele.org/daemon/daemontest` only from daemoncmd, so an external
  `package daemontest_test` fails lint; daemontest's own tests are `package daemontest`.
- `daemontest.Start` (daemon.go) returns once the WithReady hook fires or Run returns. With a
  `HangStart` runner neither happens until the start timeout, and the test cannot call
  `Clock.Advance` before Start returns, so start-timeout tests drive `daemon.New(...).Run`
  directly (see runner_test.go).
- Start's cleanup releases every `*daemontest.Runner` in Config.Runners before Stop, so an
  IgnoreStop runner without a fake clock does not hold cleanup for the 10s default grace; an
  explicit `Stop()` does not release, so it still observes ErrStopTimeout.
- `Clock.Advance` (clock.go) starts due callbacks in deadline order, each in its own goroutine, and
  does not wait for them; `BlockUntilTimers` wakes on a channel replaced on every arm/fire/stop.

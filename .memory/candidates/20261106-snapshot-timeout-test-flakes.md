---
about: "TestSnapshotTimeout in source/daemon/health_test.go flakes: it requires a timed-out check's measured Duration to be at least the 1ms timeout, and it was seen at about 600µs"
saw:
  - source/daemon/health_test.go
---

- `TestSnapshotTimeout` (source/daemon/health_test.go, the `r.Duration < time.Millisecond` assertion) failed once under `go test -race -shuffle=on ./...` with `check "honours-context": Duration = 601.899µs, want at least the timeout`; it passed on rerun and with `-count=20`. The check's measured duration can come out shorter than the `WithCheckTimeout(time.Millisecond)` deadline, so the lower bound is timing-dependent. A lone failure of this test in a run is this flake, not a regression in the change under test.

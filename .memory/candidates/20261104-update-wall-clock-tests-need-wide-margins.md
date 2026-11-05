---
about: "update's wall-clock tests flake under -race and parallel load unless each bound has wide slack; bounded-wait tests should block until ctx ends instead"
saw:
  - source/update/github_test.go
  - source/update/background_test.go
  - source/update/checkhooks_test.go
---

Verified in code, 2026-11-04.

- `TestGitHubSource_TimeoutIsPerRequest` (github_test.go) proves `GitHubSource.Timeout` bounds each
  request separately: two sleeping handlers whose sum exceeds Timeout. With 300ms sleeps under a
  500ms Timeout (200ms slack per request) it failed under `go test -race -shuffle=on ./...`; it
  now sleeps 1.1s under 2s. A sleep never returns early, so the sum-exceeds-Timeout half of the
  proof holds under any load; only the per-request slack needs to be wide.
- The tests that assert a bound (Timeout, CheckWait) use a handler or `updatetest.Slow` gate that
  blocks until the request context ends, with short deadlines: load only delays the deadline, so
  there is no margin to lose. Assertions on elapsed time use a generous ceiling (`< 1s`), never a
  tight one.
- The background-check and hook tests that need the check to finish within CheckWait use
  `CheckWait = 2s` (`bgConfig` in background_test.go) so a fast fake server always wins.
- No `runtime.NumGoroutine` assertions next to an httptest server: its connection goroutines make
  them flaky. `Finish` returning is what proves the check goroutine was joined.

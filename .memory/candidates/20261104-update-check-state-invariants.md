---
about: "update background check: only a completed attempt is saved, the check re-throttles under the filelock, the file store honours ctx by abandoning its I/O, and Finish is the only wait"
saw:
  - source/update/check.go
  - source/update/state.go
  - source/update/background.go
  - source/update/config.go
  - source/update/github.go
  - docs/adr/0023-update-keeps-its-check-state-through-cli-state.md
  - CONTEXT.md
---

Verified in code, 2026-11-04.

- `afterAttempt` (check.go): when ctx is done or the error is `context.Canceled` it returns the
  state unchanged and "do not save". A ctx deadline surfaces as `*TimeoutError`, like the source's
  own per-request timeout, which is why ctx is consulted directly: a request timeout under a live
  ctx is an attempt. CONTEXT.md "Check attempt" says a check stopped past the bound the process
  waits for is not an attempt.
- `Config.Start` (background.go) loads the state synchronously; a Load error counts as the zero
  State. Ineligible returns a nil `*Check`, which every method accepts. The goroutine runs on
  `context.WithCancel(context.WithoutCancel(ctx))`, so only `Finish`/`cancelNow` stop it.
- `runCheck` (background.go): Lock, reload (an error abandons: no query, no save), re-throttle with
  `throttleElapsed` only (not full eligibility, so Getenv/IsTerminal never run on the goroutine);
  when another process has just checked, the reloaded LatestVersion is this run's result, with no
  query and no save.
- `Finish` waits on a real `time.NewTimer(CheckWait)` (not the `Now` seam, which stamps state
  only), then cancels and joins. The bound holds only while Locker, Source and StateStore honour
  ctx.
- `fileStore` (state.go) takes no lock; Save is MkdirAll 0700 + cli/state's atomic WriteFile (temp
  + rename, last rename wins). Load and Save run the I/O on a goroutine through `within` and return
  ctx.Err() when ctx ends first; the abandoned write finishes on its own, atomically. Serialising
  the check is the filelock of ADR-0019, taken in `runCheck`.
- The default `Config.Locker` is `dirLocker` (config.go): it MkdirAll's StateDir 0700 first,
  because filelock refuses a missing directory and nothing else creates StateDir before the first
  Save. update requires `go.vipengele.org/filelock` through `replace => ../filelock`.
- `GitHubSource.get` (github.go) applies `context.WithTimeout` per request, so cancellation reaches
  the HTTP request.

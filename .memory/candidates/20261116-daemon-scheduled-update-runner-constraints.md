---
about: "What binds daemon/selfupdate's scheduled Runner: depguard exact-match allows for update and its test kit, a reload that must never error, a cycle timeout whose ctx.Err is Canceled with DeadlineExceeded only as the cause, WithRunnerClock because WithClock is the Machine's, and a RestoreError that only a seam can produce in tests"
saw:
  - source/daemon/.golangci.yml
  - source/daemon/go.mod
  - source/daemon/reload.go
  - source/daemon/selfupdate/runner.go
  - source/daemon/selfupdate/options.go
  - source/daemon/selfupdate/cycle.go
  - source/update/errors.go
  - docs/adr/0025-a-daemon-reloads-its-config-on-sighup-through-an-opt-in-option.md
---

Checked 2026-11-16 after the scheduled self-update runner landed.

- daemon requires `go.vipengele.org/update` with `replace => ../update` (go.mod:9, go.mod:29). The
  selfupdate depguard rule allows `update$` and `update/updatetest$` by exact match
  (.golangci.yml:50-51) under a blanket deny of `go.vipengele.org/`, test files included: any other
  update subpackage a selfupdate file imports needs its own entry.
- A Reload error is fatal to the whole daemon run (reload.go, ADR-0025), so `(*runner).reload`
  (runner.go:263) keeps the old interval and warns on an invalid one and `Runner[T].Reload` always
  returns nil. Reload runs concurrently with the timer callback, hence the runner's mutex.
- The per-cycle timeout is a clock timer calling `cancel(context.DeadlineExceeded)` on a
  WithCancelCause context (runner.go:326): inside a timed-out cycle `ctx.Err()` is
  `context.Canceled` and only `context.Cause(ctx)` is DeadlineExceeded. A cycle that wants to tell a
  timeout from Stop must read the cause.
- The runner's clock option is `WithRunnerClock` (options.go:91): `WithClock` (options.go:23) is the
  Machine's `Option` and a different type.
- Missing release: `update.ErrNoRelease` (errors.go:22); the GitHubSource returns it for an empty
  beta/edge list and a stable `/releases/latest` 404, so on stable a misnamed or private repo also
  logs at debug.
- A real `*update.RestoreError` cannot be produced from outside update, so cycle.go carries an
  unexported `apply` func field (default `applyWithHook`) that the RestoreError test replaces.

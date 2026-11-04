---
about: "update check state: only a completed attempt is recorded, store is lock-free atomic last-wins, no Start/Notice exists yet, ctx honoured through HTTP"
saw:
  - source/update/check.go
  - source/update/state.go
  - source/update/github.go
  - docs/adr/0023-update-keeps-its-check-state-through-cli-state.md
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - CONTEXT.md
---

For issue #55 (background Start/Notice). Verified in code, 2026-11-04.

- afterAttempt (check.go, end of file): if ctx.Err()!=nil or errors.Is(err, context.Canceled) -> returns state unchanged, false (no save). Ctx is consulted because a ctx deadline surfaces as *TimeoutError, same as the source's own per-request timeout; a request timeout with live ctx IS an attempt. Any attempt sets LastAttempt=now UTC; LatestVersion updated only when err==nil. CONTEXT.md "Check attempt" (~l.269) also says "past the bound the process waits for it" is not an attempt.
- fileStore (state.go) takes no lock; Save = MkdirAll 0700 + state.SaveJSON -> state.WriteFile (cli/state/write.go:61, temp file + fsync + rename, atomic); last rename wins. ADR-0023 "No lock around the file": serialising the check is the filelock Lock of ADR-0019, taken by whoever runs the check. update's go.mod does NOT yet require filelock (grep 0 hits). Load/Save ignore ctx.
- ADR-0008: update may depend only on cli/spec and commons; ADR-0023 amends it for cli/state. Terminal detection already is a seam: Config.IsTerminal (config.go:137, default via cli/spec/terminal.Detect), so x/term is only an indirect dep via cli/spec; no new dep needed.
- GitHubSource.get (github.go:314) wraps ctx with WithTimeout and uses NewRequestWithContext: cancellation reaches the HTTP request. Source.Latest(ctx) interface (source.go).
- No Start/Notice exist in source/update. No ADR assigns stderr notice printing to library vs CLI; ADR-0013 only says messages go to stderr. Hook ADR-0009 (the-app-owns-version-and-takes-hooks): update check is a tool-owned Hook. goleak is not used anywhere; repo idiom is runtime.NumGoroutine baseline + waitGoroutines polling (cli/core/execute_test.go:472).

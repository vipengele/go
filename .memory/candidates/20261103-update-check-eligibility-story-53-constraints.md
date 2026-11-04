---
about: "source/update decides a background check from a pure eligibility(checkInputs) and records it with afterAttempt, which consults the check's ctx because a ctx deadline surfaces as *TimeoutError"
saw:
  - source/update/check.go
  - source/update/config.go
  - source/update/errors.go
  - source/update/.golangci.yml
---
- Gates, first failing wins, in `checkReason` order: dev build (`!version.IsRelease(Version)`),
  `CI` non-empty, stderr not a terminal (`Config.IsTerminal`), `Config.DisableCheck`, the opt-out
  variable `Identity.EnvVar(optOutSuffix)` non-empty ("0" included), throttle (check.go
  `eligibility`, `throttleElapsed`). The throttle passes on a zero LastAttempt, on a LastAttempt
  after Now (clock moved back), or on elapsed >= Interval.
- `checkInputsFrom(r Config, st State)` reads every input through the resolved Config's seams
  (Getenv, Now, IsTerminal), so tests fake them without touching the environment (check.go).
- `afterAttempt` returns (st, false) when `ctx.Err() != nil` or `errors.Is(err, context.Canceled)`.
  The error alone cannot tell a command-end deadline from the source's own per-request timeout:
  `classifyError` (errors.go) turns any deadline into `*TimeoutError`. A failed attempt keeps
  LatestVersion (check.go `afterAttempt`).
- `spec.Identity.EnvVar` returns `(string, error)`; Config embeds Identity, and staticcheck
  QF1008 rejects `r.Identity.EnvVar`, so the call is `r.EnvVar` (check.go `optedOut`).
- CheckInterval: 0 resolves to 24h; negative or under 1h fails Validate as a FieldError wrapping
  validate.ErrInvalid (config.go `checkInterval`, `defaultCheckInterval`, `minCheckInterval`).

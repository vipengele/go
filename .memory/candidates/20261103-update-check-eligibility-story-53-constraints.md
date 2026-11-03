---
about: "Issue #53 (check eligibility/throttle) in source/update: no Version/state/CI/terminal/interval exists yet; every seam must be a Config field resolved in resolve(); interval-min belongs in Config.Validate"
saw:
  - source/update/config.go
  - source/update/AGENTS.md
  - source/update/.golangci.yml
  - source/update/errors.go
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
  - docs/adr/0018-the-state-store-is-its-own-project-cli-state.md
  - source/cli/spec/env.go
---
Memory notes matched nothing (only unrelated CI notes); explored directly.

- Conventions (AGENTS.md "Conventions"): outside world only through a Config field, zero = production default set in `resolve()` (config.go ~L200-290); gochecknoglobals on (sentinels ok); depguard denies cli/core/** and cobra outside updatecmd (.golangci.yml). Default linters "standard" + depguard + gochecknoglobals; no paralleltest, but tests use t.Parallel throughout.
- Opt-out var already modelled: `optOutSuffix = "NO_UPDATE_CHECK"` (config.go:22), name via `Identity.EnvVar` (ADR-0008). Config has Now, Getenv, StateDir, InContainer, but NO Version field (ADR-0003 sketch with Version/Token is historical), no CI/terminal/interval/enabled/state fields.
- Validation: `Config.Validate` (config.go) joins FieldErrors wrapping validate.ErrInvalid via `checkNotNegative`-style helpers; resolve() calls Validate first. A min-interval check naturally is a new Validate helper (FieldError + ErrInvalid), zero => default.
- Terminal: cli/spec `Env.Terminal func(Stream) bool` (spec/env.go:24-28); dev build: `cli/spec/version.IsRelease(v)`; update already imports cli/spec.
- ADR-0018: cli/state exists; "whether update requires it is left to issue #54" and update's ADR-0005 limit is cli/spec only. ADR-0003 says update owns "check throttling and its state". So #53 should take last-attempt as plain input (pure function), not a store interface.
- Cancellation: classifyError (errors.go:~333) passes context.Canceled through unchanged; download/extract/github doc say errors match context.Canceled (github.go:78, download.go:63, extract.go:83), so errors.Is works. Nothing in this repo records the gt "cancelled-but-marked-checked" gotcha.

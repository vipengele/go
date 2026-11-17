---
about: "what a new project needs, and that commons is stdlib-only with no retry, rate-limit, circuit-breaker or logging abstraction anywhere"
saw:
  - .lydite/components.yml
  - .gt-repo.yaml
  - go.work
  - source/commons/doc.go
  - source/daemon/clock.go
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - docs/adr/0018-logging-is-an-opt-in-app-option.md
  - docs/adr/0028-lifecycle-time-and-readiness-are-injectable-seams.md
---
Checked 2026-11-17 by reading go.mod of every project, ADRs and `grep -rli retry|backoff|ratelimit|slog`.
- A new project touches: `source/<path>/go.mod` (module `go.vipengele.org/<path>`, `go 1.27.4` currently; ADR-0004 text says 1.27.2, rule file says full patch), its own `.golangci.yml` (commons: `default: standard` + `gochecknoglobals`; update adds depguard), `.lydite/components.yml` (`runner: go-test`, `args: ["-race","./..."]`), `.gt-repo.yaml` gomod entry + `gt repo sync`, `go.work` `use` line, release notes `docs/release-notes/<path>/vX.Y.Z.md`.
- No policy ADR on third-party deps. Convention only: commons "requires nothing beyond the standard library" (`source/commons/doc.go`); filelock requires only x/sys; config only yaml.in/yaml/v3; cli/core is the sole cobra user (ADR-0003/0005); update has depguard forbidding cli/core and cobra.
- No retry/backoff/jitter/rate-limiter/circuit-breaker code exists. Nearest: update/github.go parses Retry-After/X-RateLimit headers (`:593-615`); filelock has a shared retry loop on lock open. Injectable clock exists only inside daemon (`daemon.Clock` {Now, AfterFunc}, ADR-0028 lifecycle seams, fake in `daemon/daemontest`), not shared. Logging is slog, in `cli/core/logging` with a redacting handler (ADR-0018); daemon/httpserver is a server kit (ADR-0026), no client.

---
about: "What a daemon health model must respect: Supervisor keeps no public per-runner state, Runner has no health method, health is explicitly out of scope of ADR-0014/0021, daemon cannot import cobra/cli/core and has an empty go.mod"
saw:
  - source/daemon/supervisor.go
  - source/daemon/runner.go
  - source/daemon/.golangci.yml
  - source/daemon/go.mod
  - docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md
  - docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md
  - docs/adr/0014-json-output-envelope.md
  - CONTEXT.md
---

Checked 2026-11-03 for issue vipengele/go#71 (health and status model).

- Per-runner state exists only as unexported `launched.state` (starting/running/failed) and `launched.exited`, written "only by the goroutine running Run" (supervisor.go, `launched` struct). `Supervisor` exposes no accessor; `Run` is single-shot (`ran` atomic, ErrAlreadyRan). A health snapshot read from another goroutine would need new synchronization (those fields are unsynchronized) or a derivation from `Runner.Done()`/`Err()`, which are safe (closed channel; Err nil before Done).
- Runner interface (runner.go) has no health method; only optional `Order() int` via an unexported `orderer` assertion. A health check is therefore an additive optional interface or a registration, not a change to Runner.
- ADR-0014 and ADR-0021 both list health as out of scope / "later stories"; CONTEXT.md Supervisor definition and ADR-0003 table assign health to daemon ("owns the process lifecycle and health"). No ADR decides a health design; no code or note mentions liveness/readiness.
- Readiness already has a lifecycle meaning: `Start` returns when the runner is ready, later runners start only after (ADR-0021). `Run` blocks, so "all Starts returned" is the supervisor's natural ready edge; `waiting` hook (supervisor.go) fires each wait-loop pass. Shutdown order: reverse start, HTTP runner Order 100 stops first (http.go DefaultHTTPOrder), so a health handler on that runner stops serving before the work it reports on.
- Package constraints: go.mod has no require (stdlib only); `.golangci.yml` depguard denies cobra and `go.vipengele.org/cli/core/**` everywhere; gochecknoglobals on. ADR-0005 puts commands in `cli/spec` (framework-neutral `spec.Command`), cobra only in `cli/core/internal/cobra`, daemon requires cli/core, cli/spec, update per its dependency graph (ADR-0003 text still says daemoncmd with cobra; ADR-0005 supersedes). So a `status` command would be a `[]*spec.Command` from a daemon subpackage requiring `cli/spec` (needs `require` + directory replace `../cli/spec`, ADR-0006), never cli/core.
- `--json` convention: ADR-0014-json-output-envelope: flat object led by `schema` int, written by `spec.WriteJSON(inv.Env.Stdout, schema, v)`; command reads `inv.Output.Format()`, not its own flag (version is the sole exception); keys pinned with `speccheck.AssertJSONKeys`. Data on stdout (ADR-0013). An HTTP handler can't use spec.WriteJSON's writer contract directly but should mirror the envelope (schema first) so one struct serves both.
- Issues #73 (HTTP server kit: constructor defaults, middleware, JSON error envelope) and #74 (test kit: ephemeral 127.0.0.1:0 daemon, fake runner fail/hang/ignore-stop) were read via REST; neither mentions health. #74 fake runner would be the way to test runner states.
- tumika prior art (health.go, api/health.go) is outside this repo; nothing in the repo or store records its design or bug-fix history. Unverified.

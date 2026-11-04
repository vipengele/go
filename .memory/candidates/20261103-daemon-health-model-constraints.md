---
about: "What the daemon health model rests on: supervisor health state is a separate mutex-guarded feed, launched stays Run-goroutine-only, the HTTP Runner stops first, and the root package takes only spec.Identity, cli/spec/paths and filelock while daemoncmd uses the rest of cli/spec"
saw:
  - source/daemon/supervisor.go
  - source/daemon/runner.go
  - source/daemon/health.go
  - source/daemon/health_http.go
  - source/daemon/.golangci.yml
  - source/daemon/go.mod
  - source/daemon/daemoncmd/status.go
  - docs/adr/0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md
  - docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md
  - docs/adr/0014-json-output-envelope.md
---

- `launched` (supervisor.go) is written and read only by the goroutine running `Run`, with no
  synchronisation. Health reads run state from any goroutine through `feed` (supervisor.go), a
  separate mutex-guarded struct `Run` publishes to at each transition (`runPhase`,
  `runnerPhase`). A health check touching `launched` races.
- `feed.ready` and `feed.running` also treat a Runner whose `Done` has closed as exited before
  `Run` has handled the exit, so `HealthChecker.CheckHealth` (runner.go) is never called on an
  exited Runner whatever the timing.
- The HTTP Runner (http.go, `DefaultHTTPOrder` 100) starts last and stops first, so a health
  route mounted on it stops answering before the work it reports on
  (docs/adr/0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md, Consequences).
- The root package imports from siblings only `cli/spec` (`spec.Identity`), `cli/spec/paths` and
  `filelock`, for the instance lock
  (docs/adr/0023-a-daemons-single-instance-is-a-filelock-try-lock-with-a-holder-record.md); only
  `daemoncmd` imports other `cli/spec` packages. `.golangci.yml` enforces it with a depguard rule
  over `!**/daemoncmd/**` that allows exactly those, plus `daemon` and `daemon/service`. Commands
  are `*spec.Command` from cli/spec, never cli/core (ADR-0005).
- `writeEnvelope` (health_http.go) duplicates `spec.WriteJSON`'s layout (cli/spec/json.go) even
  though the root package imports cli/spec; `daemoncmd/status_test.go` compares the command's
  JSON with the handler's `/status` body byte for byte, which is what catches drift.

---
about: "What the daemon health model rests on: supervisor health state is a separate mutex-guarded feed, launched stays Run-goroutine-only, the HTTP Runner stops first, and the root package stays stdlib-only while daemoncmd requires cli/spec"
saw:
  - source/daemon/supervisor.go
  - source/daemon/runner.go
  - source/daemon/health.go
  - source/daemon/health_http.go
  - source/daemon/.golangci.yml
  - source/daemon/go.mod
  - source/daemon/daemoncmd/status.go
  - docs/adr/0023-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md
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
  (docs/adr/0023-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md, Consequences).
- The root package imports only the standard library; `daemoncmd` alone requires `cli/spec`
  (go.mod, directory replaces for `cli/spec` and `commons`). `.golangci.yml` enforces it with a
  depguard rule over `!**/daemoncmd/**`. Commands are `*spec.Command` from cli/spec, never
  cli/core (ADR-0005).
- `writeEnvelope` (health_http.go) duplicates `spec.WriteJSON`'s layout (cli/spec/json.go)
  because the root package cannot import cli/spec; `daemoncmd/status_test.go` compares the
  command's JSON with the handler's `/status` body byte for byte, which is what catches drift.

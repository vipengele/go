# Health is checked on demand, with liveness and readiness kept distinct

Status: accepted. It decides the health that
`docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md` and
`docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md` leave out. The
Runner contract and the shutdown order of those ADRs stand.

An orchestrator asks a daemon two different questions: should this process be restarted, and
should it receive traffic. A single "healthy" answer conflates them: a database outage that makes
the daemon unready would, read as a liveness failure, restart a process that restarting cannot
help. `go.vipengele.org/daemon` keeps the two apart, runs its checks when asked, and publishes the
result as one **Health snapshot** over HTTP and through a `status` command.

## The rule

- **Supervisor feed.** `WithHealth(h)` makes the Supervisor register two built-in checks,
  `supervisor.live`, a liveness check, and `supervisor.ready`, a readiness check, from state
  guarded by a mutex and separate from the fields the `Run` goroutine alone writes.
  `supervisor.ready` passes only when every `Start` has returned, no Runner has exited or been
  isolated, and shutdown has not begun; it fails while starting, during shutdown and after an
  isolated panic. `supervisor.live` passes while `Run` is between the beginning of its start phase
  and its return. An isolated panic does not fail liveness: isolation exists to keep the process
  up. A Runner with a `CheckHealth(ctx) error` method is registered as a readiness check named
  after the Runner, and the method is called only while that Runner is running; at any other time
  the check fails without calling it. A Runner name already held by the `Health`, including
  `supervisor.live` and `supervisor.ready`, makes `New` fail. Registration is the last step of
  `New`, all or none, so a `New` that fails leaves the `Health` as it was.
- **Vocabulary.** A check result is `pass` or `fail`, with an error text and a duration. The
  overall `status` is `down` when any liveness check fails, else `unready` when any readiness check
  fails, else `ok`. The snapshot also carries `live` and `ready` booleans and the uptime since the
  `Health` was created. A check with no stated kind is a readiness check: liveness holds only what
  the process itself knows, and no downstream dependency. Taking a snapshot never returns an error;
  a check that panics is recovered as a `fail`.
- **Execution.** A snapshot runs every check concurrently when it is requested, each under its own
  timeout, two seconds unless `WithCheckTimeout` sets another. A check that outlasts it fails with
  the timeout and is left to return on its own; until it does, that check is not started again,
  and every snapshot in the meantime reports it as failing with the timeout without calling it.
  Nothing is cached and nothing runs in the background.
- **HTTP.** `Health.Handler()` is a plain `http.Handler` serving three paths. `/livez` runs only
  the liveness checks, a check of both kinds included, and answers 200 `ok` or 503 `down`.
  `/readyz` runs every check and answers 200 or 503 with the overall status: `ok`, `unready` or
  `down`. `/status` always
  answers 200, with `application/json` and the envelope of
  `docs/adr/0014-json-output-envelope.md`. Every
  response carries `Cache-Control: no-store`, and a method other than GET or HEAD is 405. Where the
  handler is mounted, and whether probes bypass authentication or middleware, is the consumer's.
- **Exit codes.** The `status` command exits 0 only when the overall status is `ok`. It exits 1
  when the status is `unready` or `down`, after printing the snapshot, and when the daemon is
  unreachable, with the error on stderr.
- **Wire contract.** The keys of the snapshot are lower snake_case and exactly these:
  - `status`: `ok`, `unready` or `down`;
  - `live` and `ready`: booleans;
  - `started_at`: an RFC 3339 timestamp in UTC;
  - `uptime_seconds`: an integer, truncated;
  - `checks`: an array, sorted by `name`, of objects with `name`; `kinds`, an array of
    `"liveness"` and `"readiness"`; `status`, `pass` or `fail`; `error`, omitted when the check
    passes; and `duration_ms`, an integer, truncated.

  The body is the envelope `{"schema":1,...}`: a flat object with `schema` first, indented by two
  spaces and ending in a newline, the layout `spec.WriteJSON` produces. The handler's `/status`
  body and the output of the `status` command under `--json` are byte-identical. A key renamed,
  removed or retyped increments `schema`; an added key does not.
- **Boundary.** `Snapshot` and `CheckResult` have no `schema` field, because `spec.WriteJSON`
  refuses a value that already carries one. The root `daemon` package takes from `cli/spec` only
  what the instance lock of
  `docs/adr/0023-a-daemons-single-instance-is-a-filelock-try-lock-with-a-holder-record.md` needs,
  `spec.Identity` and `cli/spec/paths`, and `spec.WriteJSON`: the handler and the `status` command
  of `daemon/daemoncmd` both write the envelope with `spec.WriteJSON` at the same schema. Tests on
  both sides pin the same keys and bytes.

## Considered options

- **Background polling, or a cache with a time to live.** A cached answer reports a state that
  has already changed, and a poller is a goroutine the Supervisor would have to start, stop and
  order. Checks run when asked; a caller that needs a cache wraps its own check.
- **A `degraded` level between `ok` and `unready`.** An orchestrator acts on a binary answer, so
  every consumer would have to decide whether `degraded` is ready. A partial failure is a failing
  check in the snapshot, where an operator can read it.
- **Text negotiation on `/status`.** Content negotiation adds a second representation to pin and
  test. `/livez` and `/readyz` are the text routes; `/status` is the data, and the `status`
  command renders text from it.
- **Distinct exit codes per state.** A script that needs the state reads the snapshot; the exit
  code answers one question, whether the daemon is `ok`. Exit codes are `cli/core`'s
  (`docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md`),
  and a state-specific code would be one more value for it to register.
- **Failing liveness on an isolated panic.** `PanicIsolate` exists to keep the process up while
  one Runner is gone. Failing liveness would have the orchestrator kill the process the policy was
  chosen to preserve; the loss shows in readiness instead.
- **One `supervisor` check with both kinds.** Any failing check of the liveness kind makes the
  snapshot `down`, so a single check carrying both kinds fails liveness whenever it fails
  readiness, and cannot report live but unready: while starting, while stopping and after an
  isolated panic. Two checks, one per kind, keep the two answers apart.
- **A `schema` field inside `Snapshot`.** The type would then be marshalled alone as a
  different shape from the one on the wire, and `spec.WriteJSON` rejects a value that carries
  `schema`. The envelope is added at the write.
- **Downstream dependencies in liveness.** A failed dependency would restart every daemon that
  uses it, which turns one outage into a restart loop. Liveness holds only what the process knows
  of itself.
- **A local envelope writer in the root package.** It would duplicate `spec.WriteJSON`, and the
  two could drift apart while the handler's body and the command's output are meant to be
  byte-identical. The root package already imports `cli/spec` for the instance lock.

## Consequences

- **A restart and a wait are different decisions.** A dependency outage makes the daemon
  `unready` and leaves it `live`.
- **A snapshot costs the work of its checks.** Each request to `/status` or `/readyz` runs every
  check, and each request to `/livez` every liveness check, so a slow check is bounded by its
  timeout, not hidden by a cache.
- **A dependency cannot slow the liveness probe.** `/livez` runs no readiness check, so a hung
  dependency, which costs `/readyz` up to the check timeout, leaves `/livez` as fast as the
  process's own checks. An orchestrator's liveness probe timeout, one second by default in
  Kubernetes, below the two-second check timeout, would otherwise turn a dependency outage into
  restarts.
- **A check that ignores its context holds one goroutine.** An abandoned run is never joined by a
  second run of the same check, so a stuck check costs one goroutine however often probes ask.
- **A health route on the HTTP Runner stops answering first.** That Runner stops before the
  Runners started earlier (`docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md`), so the route goes away before the work it reports on.

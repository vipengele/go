# A daemon's update status is reported outside the check model

Status: accepted. It extends the wire contract of
`0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md` with one optional object,
and reports the record of `0033-a-daemon-self-update-restores-the-binary-before-recording-the-rollback.md`
to an operator.

A daemon that updates its own binary keeps a record of the update: pending, confirmed, rolled back or
idle. An operator asking why a daemon runs the version it does, or whether its last update was
undone, has to read that record from the host. `go.vipengele.org/daemon` publishes it in the
`/status` snapshot and through an `update-status` command that still answers when the daemon is not
running.

## The rules

- **The consumer supplies the channel and the watermark as strings.** `selfupdate.Reporter(store,
  release)` reads the record from the `Store` and takes `selfupdate.ReleaseInfo`, a `Channel` and a
  `Watermark`, from the `release` function the consumer passes, or from nothing when it is nil. The
  `daemon` project requires no `go.vipengele.org/update`.
- **`/status` gains a top-level `update` object.** It is present only when the `Health` was built
  with `WithUpdateReporter`. It never changes `status`, `live` or `ready`, and the schema stays 1: a
  key added does not increment it (ADR-0025).
- **The reporter runs for `/status` and `Health.Snapshot` only.** `/livez` and `/readyz` never call
  it, so a slow store cannot slow a probe.
- **The reporter is bounded like a check.** It runs under the check timeout, a panic is recovered,
  and at most one call is in flight: a reporter that is stuck is not called again, and every
  snapshot meanwhile reports `unknown` with an `error` saying why.
- **The wire fields.** `status` is always present: `idle`, `pending`, `confirmed`, `rolled_back` or
  `unknown`. Then, each omitted when empty: `from_version`, `to_version`, `boot_attempts`, `reason`,
  `started_at` and `updated_at` (RFC 3339 in UTC, to the second), `channel`, `watermark` and
  `error`. The target and backup paths of the record are deliberately not exposed.
- **`unknown` is an unreadable record.** A record that is corrupt, an I/O failure, a done context, a
  reporter timeout or panic, and any state the daemon does not recognise are all `unknown`. An
  update with nothing in progress is always spelled `idle`, so an unreadable record is never
  mistaken for one (ADR-0033: a corrupt record is an error, never idle). A `release` function that
  fails while the record is readable keeps the real `status` and carries the failure in `error`.
- **`update-status` asks the daemon first, then the store.** The command is
  `daemoncmd.NewUpdateStatus(resolve, fallback)`. It falls back to the `UpdateReporter` it was given
  on a transport error, a timeout, a resolve error, an answer other than 200 or an unreadable body,
  and when a reachable daemon reports no `update`. A cancelled parent context is not a fallback: the
  command returns its error.
- **Its JSON is one flat object.** `schema`, `source` (`daemon`, `store` or `none`), the fields of
  the update, and `daemon_error` only when a failure of the daemon caused the fallback. `none` is a
  reachable daemon that reports no update where there is no fallback; it exits 0. The note that the
  daemon was not used goes to stderr.
- **The command is flat.** `update-status` is mounted beside `status`, not in a group.
- **Exit codes.** 0 for `idle`, `pending`, `confirmed` and `none`. 1 after printing, as
  `spec.Reported(ErrUpdateNotOK)`, for `rolled_back`, for `unknown` and for a status carrying an
  `error`. 1 with a plain error when nothing could be read. The `status` command's exit code
  ignores the update.

## Considered options

- **`daemon` requiring `go.vipengele.org/update`.** ADR-0003 and ADR-0033 allow `daemon` to require
  `update`, and the type that holds the channel and the watermark is `update`'s. It would bring
  `update` and what it requires into every daemon, and tie each `daemon` release to an `update`
  release (ADR-0006), to carry two strings the consumer already holds where it builds `update`'s
  configuration. A function returning plain strings costs the consumer one closure.
- **The update as a health check.** A check is `pass` or `fail`, and a failing readiness check makes
  the daemon `unready` and a failing liveness check `down`. A rolled-back update leaves a healthy
  daemon on the restored binary, which must not stop traffic or cause a restart. `pending` is no
  failure either. The update is data beside the checks, and affects none of them.
- **A command that reads only the store, or only the daemon.** The store alone says nothing about
  what the running daemon holds, and the daemon alone says nothing when it is stopped, which is when
  an operator most needs the answer after a rollback. Asking the daemon first and the store second
  gives the running state when there is one.
- **An `update status` subcommand in an `update` group.** It would be a group holding one
  command, and a tool's `update` command already names another action. A flat `update-status`
  mounts beside `status` and `service-status`.
- **An informational exit, always 0.** A script or a monitor reading the exit code would never see a
  rollback. The printed status stays the detail; the code answers whether the update is ok.

## Consequences

- **After a rollback, `update-status` exits 1 until the next update begins**, though the daemon is
  healthy on the restored binary. The record stays `rolled_back` until `Begin` replaces it, and the
  exit code follows the record.
- **A consumer wires the reporter twice.** `daemon.WithUpdateReporter(selfupdate.Reporter(store,
  release))` on the daemon side and `daemoncmd.NewUpdateStatus(resolve,
  selfupdate.Reporter(store, release))` on the command side. `daemoncmd` does not import
  `selfupdate`: the consumer passes the reporter.
- **The reporter only reads.** `selfupdate.Reporter` calls `Load` and writes nothing, so a status
  request cannot change the record the rollback depends on.
- **An update status costs the work of its reporter.** Each `/status` request calls it once under
  the check timeout; a probe never does.

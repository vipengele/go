# Systemd readiness and socket activation use the standard library and an injected environment

Status: accepted. It takes up the systemd readiness notification and socket activation that
`0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md` lists as
out of scope, and keeps to `0024-service-drivers-are-selected-at-runtime.md`
(runtime `GOOS` selection, no build tags) and to
`0028-driver-specific-settings-are-options-not-config-fields.md` (a driver setting is an option, not
a `Config` field).

A daemon run as a systemd service can tell the manager when it is ready (`READY=1`), that it is
stopping (`STOPPING=1`) and that it is alive (`WATCHDOG=1`) with datagrams on the socket named by
`NOTIFY_SOCKET`, and it can receive listening sockets that systemd opened for it, announced by
`LISTEN_PID`, `LISTEN_FDS` and `LISTEN_FDNAMES`. The `daemon/systemd` package holds the protocol;
`daemon.WithSystemd(lookup)` is the `Option` of the Supervisor that wires it to the run. Both are
opt-in: nothing is detected automatically.

## The rules

- **The protocol is the standard library.** `daemon/systemd` uses `net`, `os` and `runtime` and no
  third-party module.
- **The environment is injected.** Every entry point takes a lookup function,
  `func(key string) (string, bool)`, the shape of `spec.Env.LookupEnv`, so a caller passes
  `env.LookupEnv` and `daemon/systemd` imports no `cli/spec`. The code never calls `os.Getenv` and
  never changes the process environment, so the variables stay visible to child processes and a test
  supplies its own.
- **Off Linux and outside systemd everything is a no-op.** Each entry point does nothing when
  `runtime.GOOS` is not `linux` or when its variables are absent, so a cross-platform daemon keeps one
  code path (ADR-0024).
- **A notify failure never fails `Run`.** A failed send is logged by the Supervisor's logger and the
  run goes on: the daemon works whether or not systemd hears about it.
- **Readiness follows the starts.** `READY=1` is sent once, on the goroutine of `Run`, at the point
  where `WithReady` is called, after every Runner has started. `WithSystemd` keeps its own state and
  does not replace `WithReady`.
- **`STOPPING=1` is sent whenever the stop phase begins.** That includes a stop after a failed start
  phase, which sent no `READY=1`. A run that fails on the instance lock returns before either.
- **The watchdog is gated on liveness.** When `WATCHDOG_USEC` is set, and `WATCHDOG_PID` if set equals
  the process id, the Supervisor runs the liveness checks of its `Health` every `WATCHDOG_USEC/2` on
  its injected `Clock` and sends `WATCHDOG=1` only when they pass; with no `Health` it sends it
  unconditionally. Pings start after `READY=1` and stop when the stop phase begins.
- **A hung liveness check delays the ping, and that is intended.** The checks run synchronously on the
  tick. A check that outlives the interval holds back `WATCHDOG=1`, and systemd restarts the service,
  which is what a watchdog is for.
- **A malformed watchdog setting disables the watchdog.** An unparsable `WATCHDOG_USEC` is logged as a
  warning and no ping is sent; the rest of the run is unaffected.
- **Inherited sockets are adopted by an explicit lookup.** `systemd.Listeners(lookup)` returns the
  listeners keyed by the names in `LISTEN_FDNAMES`, `unknown` where a name is missing, and an empty
  result, not an error, when the process was not activated, `LISTEN_PID` is not its own or the
  platform is not Linux. The caller hands the one it wants to `NewHTTPServerListener` and otherwise
  uses `NewHTTPServer`.
- **`Listeners` is called once per process.** It closes the inherited descriptors after wrapping
  them, so a second call would wrap whatever later reused those numbers.
- **A malformed `LISTEN_PID` or `LISTEN_FDS` is an error.** So is a descriptor that is not a stream
  socket, such as one from `ListenDatagram=`; every listener opened so far is closed before the error
  is returned.
- **`WithSystemd(nil)` is rejected by `New`,** like every sibling option.

## The service driver

The systemd driver in `daemon/service` writes `Type=simple` unless the caller passes
`service.WithSystemdNotify(watchdog time.Duration)`, which writes `Type=notify` and, when `watchdog` is
above zero, `WatchdogSec=`. It is an option on `New` and not a `Config` field, as ADR-0028 requires,
and other drivers ignore it.

It is opt-in because the driver cannot know whether the binary it registers calls `daemon.WithSystemd`.
A `Type=notify` unit whose binary never sends `READY=1` does not become active: systemd fails its
start at the start timeout.

## Considered options

- **`github.com/coreos/go-systemd`.** It implements the same protocol. Rejected: a third-party
  dependency for a few hundred lines of standard library, and it reads the process environment
  directly.
- **`os.Getenv`.** Rejected: a test would have to mutate the process environment, which is shared
  state, and the injected `spec.Env` is how the rest of the library reads it (`0012-signals-are-registered-by-execute-and-injected-through-env.md`).
- **An unconditional heartbeat.** A `WATCHDOG=1` on a timer whatever the checks say. Rejected: it
  proves only that the clock goroutine runs, so systemd never restarts a process whose liveness
  checks fail.
- **`NewHTTPServer` adopting inherited listeners on its own.** Rejected: the constructor would read
  the environment behind the caller's back, `Listeners` could be consumed twice, and a daemon with
  several activated sockets could not say which one the HTTP server takes.
- **Keeping the inherited descriptors open after wrapping.** Rejected: the process would hold a second
  descriptor for every listener, and closing the wrapped listener would not release the socket.
- **`Type=notify` by default in the driver.** Rejected: every existing registration of a binary that
  does not send `READY=1` would fail to start.

## Consequences

- **Still out of scope:** `EXTEND_TIMEOUT_USEC`, `RELOADING=1`, the file descriptor store and
  generating `.socket` units.
- **A daemon opts in at two places:** `daemon.WithSystemd(env.LookupEnv)` in the binary and
  `service.WithSystemdNotify` in the registration. `WithSystemd` without `WithSystemdNotify` is
  harmless: the unit stays `Type=simple`, systemd gives the process no `NOTIFY_SOCKET`, so nothing
  is sent. `WithSystemdNotify` without `WithSystemd` makes a unit that never becomes active,
  because systemd fails its start at the start timeout.

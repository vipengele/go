// Package daemon runs the long-running work of a process as Runners under a Supervisor, and
// keeps a second instance of the same tool from running beside it. Its subpackage daemoncmd holds
// the commands a tool built on cli/spec uses to manage its daemon.
//
// A Runner is one unit of long-running work. Start returns once the Runner is ready, leaving its
// work running; an error or a panic from Start means it failed to start. Done closes once the
// Runner has exited and Err then reports why. Stop asks it to exit and returns once it has
// released everything it holds. A Runner knows nothing of its siblings. A Supervisor, built with
// New from a list of Runners and Options, starts them together and stops them together; Run
// blocks until every Runner has been stopped.
//
// # Order
//
// A Runner may have a method Order() int; one without it has Order 0. The Supervisor starts the
// Runners in ascending Order, Runners of equal Order in the order given to New, and stops them in
// the reverse of that start order, so a Runner is stopped before the Runners it depends on.
//
// # Start
//
// Run calls each Runner's Start in turn and waits for it to return before calling the next. Every
// Start shares one context, which carries the values and the cancellation of the context passed
// to Run and is cancelled when the start phase ends, so work that outlives Start must not be bound
// to it. The whole phase is bounded by the start timeout, thirty seconds unless WithStartTimeout
// sets another.
//
// A Start that returns an error or panics is a start failure. A Start still running when the
// start timeout expires is ErrStartTimeout naming it. The context passed to Run cancelled during
// the phase ends it without a failure. In each case no further Start is called and shutdown
// begins, stopping the Runners whose Start was called.
//
// # Shutdown
//
// Once every Runner has started, shutdown begins when the context passed to Run is done or when a
// Runner exits, which its Done reports. An exit before shutdown is a failure: ErrRunnerExited when
// Err is nil, otherwise Err naming the Runner.
//
// Each Runner whose Start was called is then stopped in reverse start order. Stop is called
// exactly once on each, including one whose Start failed, panicked or has not returned. After Stop
// returns the Supervisor waits for Done to close, or, for a Runner whose Start has not returned,
// for Start to return; the return of Stop alone is never taken as the Runner having exited.
//
// The Runners share one grace period for the whole shutdown, ten seconds unless WithGrace sets
// another. It is shared, not a budget per Runner. A Runner whose Stop returns an error, or whose
// Stop, Done or Start is still pending when the grace period ends, is reported as ErrStopTimeout,
// and once it has ended Run no longer waits for the Runners after it. An abandoned Stop or Start
// may still be running when Run returns.
//
// # Panics
//
// A panic in a Runner's Start, Stop or Err is recovered into a *PanicError and never re-raised. A
// panic in Start is always a start failure, and a panic in Stop a stop problem. The PanicPolicy,
// set with WithPanicPolicy, applies only to a running Runner that exits with a *PanicError: under
// PanicFail, the default, that exit is a failure that begins shutdown; under PanicIsolate it is
// recorded and the other Runners keep running.
//
// # Func
//
// Func adapts a function to a Runner. Start launches the function in a goroutine of its own and
// returns at once. The function gets a context carrying the values of the start context but not
// its cancellation, so it keeps running when the start phase ends; Stop cancels it. A function
// that returns ctx.Err() once stopped exits with context.Canceled, a clean stop. WithOrder places
// it in the start order. A nil function makes a Runner that exits at once with ErrNilFunc.
//
// # HTTP
//
// NewHTTPServer returns a Runner that binds an *http.Server's Addr in Start, so a failure to bind
// is a start failure; NewHTTPServerListener serves on a listener the caller has already bound.
// Both have Order DefaultHTTPOrder unless WithOrder places them, so the server starts after the
// Runners at the default Order and is stopped before them. Start sets the server's BaseContext to
// a context free of the start context's cancellation, cancelled only once the drain ends. Stop
// shuts the server down: requests in flight have the rest of the grace period to finish rather
// than having their contexts cancelled when shutdown begins, and the server is closed when the
// grace period ends first. A nil server makes Start return ErrNilServer, and a nil listener
// ErrNilListener.
//
// # Single instance
//
// The instance lock lets one instance of a tool run at a time for one runtime directory. A program
// that has no Supervisor takes it with AcquireInstance and releases it with Instance.Release; a
// program run by a Supervisor passes WithInstanceLock to New, and Run takes the lock before the
// first Start and releases it as Run returns, once every Runner has been stopped or, at the end of
// the grace period, abandoned. The lock is never released between two Stops, so no second instance
// starts while a Runner of the first is still stopping. While another instance holds it, Run
// starts no Runner and returns the *AlreadyRunningError.
//
// The lock file, instance.lock, lives in the runtime directory paths.Resolve gives the tool's
// identity, which is never XDG_RUNTIME_DIR: the system removes that directory when the user's
// last session ends, and a second instance would then lock a new file. The lock alone decides
// which instance runs. Beside it the holder records its pid in instance.holder, which only
// enriches the *AlreadyRunningError, matched with errors.Is against ErrAlreadyRunning; writing
// it is best effort. Its accepted failure modes follow from that: a loser may read a previous
// holder's pid while a new holder is rewriting the record, or a stale one on Windows, where a
// rename can fail while another process reads the file; a PID of 0 means the record could not be
// read; a crash leaves at most one stray instance.holder.tmp, which the next write replaces. A
// Runner abandoned at the end of the grace period may still be running when the lock is
// released.
//
// # Health
//
// A Health, built with NewHealth, is a registry of health checks that runs them all, concurrently
// and each under its own timeout, whenever a Snapshot is asked for. A Liveness check says whether
// the process should be restarted, a Readiness check whether it should receive traffic. The
// snapshot is down when a liveness check fails, else unready when a readiness check fails, else
// ok.
//
// WithHealth has New register the Supervisor's own checks in a Health, as its last step and all
// or none, so a New that fails leaves the Health as it was. They are fed from state Run publishes
// at each step of the run:
//
//   - supervisor.live, a Liveness check, passes from the beginning of the start phase until Run
//     returns. A panic isolated under PanicIsolate does not fail it: isolation keeps the process
//     up.
//   - supervisor.ready, a Readiness check, passes once every Start has returned, while no Runner
//     has exited or been isolated and shutdown has not begun.
//   - Each Runner that is a HealthChecker gets a Readiness check named after it. The check calls
//     CheckHealth only while the Runner is running: its Start has returned nil, it has not exited
//     and shutdown has not begun. At any other time it fails without calling CheckHealth.
//
// A run is therefore down before Run and after it returns, unready while starting, while stopping
// and once a Runner's panic has been isolated, and ok in between. A Runner name already held by
// the Health, including supervisor.live and supervisor.ready, makes New fail with
// ErrDuplicateCheck.
//
// A check still running when its timeout expires fails with ErrCheckTimeout and is left to
// return on its own; until it does, later snapshots report it as failing without running it again.
//
// Handler serves a Health over HTTP: /livez and /readyz answer 200 or 503, and /status the
// snapshot as JSON. /livez runs only the Liveness checks, so a slow dependency cannot delay the
// probe that decides a restart; /readyz and /status run every check. The status command of
// package daemoncmd is a client of /status: it prints the snapshot as text, or as the same JSON
// under the App's JSON output, and fails unless it is ok.
//
// # Errors
//
// Run returns nil when its context is cancelled and every Runner stops cleanly. Otherwise it
// returns the errors joined with errors.Join, in this order: the failure that began shutdown
// (a start failure, ErrStartTimeout, ErrRunnerExited or a Runner's exit), the panic exits isolated
// under PanicIsolate in start order, then the stop problems in stop order. A Start error that only
// reports the end of its context once the start phase has ended is dropped, and so is an exit
// during shutdown whose Err wraps context.Canceled; any other exit during shutdown is a stop
// problem. With WithInstanceLock, a lock that cannot be taken is Run's only error, an
// *AlreadyRunningError while another instance holds it, and no Runner is started. A second Run
// returns ErrAlreadyRan, even after a first that returned because the lock was held: Run marks
// the Supervisor as run before it takes the lock, so a program that retries builds a new
// Supervisor with New. Match with errors.Is and errors.As.
//
// # Restart
//
// A Runner asks for the process to be restarted by exiting with an error wrapping
// ErrRestartRequired. RestartRequested reports whether every leaf of the error Run returned is
// such a restart; it is false when the restart is joined with any other problem or with a
// *PanicError. A consumer turns a requested restart into exit status 0, so a service manager
// that restarts the process does so without recording a failure:
//
//	err := sup.Run(ctx)
//	if daemon.RestartRequested(err) {
//		return nil
//	}
//	return err
//
// # Signals
//
// The package does not handle signals. The context passed to Run drives shutdown; a program that
// stops on SIGINT or SIGTERM cancels that context from its own signal handler.
package daemon

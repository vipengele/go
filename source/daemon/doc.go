// Package daemon runs the long-running work of a process as Runners under a Supervisor. It
// requires nothing beyond the standard library.
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
// # Errors
//
// Run returns nil when its context is cancelled and every Runner stops cleanly. Otherwise it
// returns the errors joined with errors.Join, in this order: the failure that began shutdown
// (a start failure, ErrStartTimeout, ErrRunnerExited or a Runner's exit), the panic exits isolated
// under PanicIsolate in start order, then the stop problems in stop order. A Start error that only
// reports the end of its context once the start phase has ended is dropped, and so is an exit
// during shutdown whose Err wraps context.Canceled; any other exit during shutdown is a stop
// problem. A second Run returns ErrAlreadyRan. Match with errors.Is and errors.As.
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

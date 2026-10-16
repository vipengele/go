// Package daemon runs the long-running work of a process as Runners under a Supervisor. It
// requires nothing beyond the standard library.
//
// A Runner is one unit of long-running work: Start blocks until its context is cancelled and Stop
// returns once the Runner has released everything it holds. A Runner knows nothing of its
// siblings. A Supervisor, built with New from a list of Runners and Options, starts them together
// and stops them together; Run blocks until every Runner has been stopped.
//
// # Shutdown
//
// Shutdown begins when the context passed to Run is done or when a Runner's Start returns before
// that, with nil (ErrRunnerExited) or with an error. The Runners are then stopped one after
// another in the reverse of the order given to New, so a Runner is stopped before the Runners it
// depends on. Each Runner's Start context is cancelled just before its Stop is called, so a
// Runner started earlier keeps running until its turn.
//
// The Runners share one grace period for the whole shutdown, ten seconds unless WithGrace sets
// another. It is shared, not a budget per Runner. A Runner whose Stop or Start has not
// returned when it ends is reported as ErrStopTimeout, and Run does not wait for the Runners
// after it. An abandoned Stop or Start may still be running when Run returns.
//
// # Panics
//
// A panic in a Runner's Start or Stop is recovered into a *PanicError and never re-raised. Under
// PanicFail, the default, a panic in Start before shutdown is a failure that begins shutdown, like
// any early return. Under PanicIsolate, set with WithPanicPolicy, it is recorded and the other
// Runners keep running. A panic during shutdown is a stop problem of its Runner under either
// policy.
//
// # Errors
//
// Run returns nil when its context is cancelled and every Runner stops cleanly. Otherwise it
// returns the errors joined with errors.Join, in this order: the failure that began shutdown, the
// isolated Start panics in start order, then the stop problems in stop order. An error from a
// Start that only reports the cancellation of its context is dropped. Match with errors.Is and
// errors.As.
//
// # Signals
//
// The package does not handle signals. The context passed to Run drives shutdown; a program that
// stops on SIGINT or SIGTERM cancels that context from its own signal handler.
package daemon

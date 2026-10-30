package daemon

import "context"

// Runner is one unit of long-running work in a daemon. It knows nothing of its sibling Runners or
// of restarts; the Supervisor starts and stops it.
//
// A Runner may also have a method Order() int. The Supervisor starts Runners in ascending Order,
// taking 0 for a Runner without the method; Runners of equal Order start in the order given to
// New.
type Runner interface {
	// Name identifies the Runner in errors and logs.
	Name() string

	// Start starts the Runner and returns once it is ready to do its work. An error, or a panic,
	// means it failed to start. ctx carries the values of the context passed to Run and is
	// cancelled when the Supervisor's start phase ends: when that context is cancelled, when the
	// start timeout expires, or once every Runner has started. Work that outlives Start must not
	// be bound to ctx.
	Start(ctx context.Context) error

	// Done is closed once the Runner has exited, whether Stop ended it or it ended by itself.
	Done() <-chan struct{}

	// Err reports why the Runner exited, once Done is closed: nil, an error, or a *PanicError for
	// work that panicked. An exit before shutdown is a failure even with nil. During shutdown an
	// error wrapping context.Canceled reports only that the Runner was stopped.
	Err() error

	// Stop asks the Runner to exit and returns once it has released everything it holds, or ctx
	// expires. The Supervisor calls it exactly once on every Runner whose Start it called,
	// including one whose Start failed, panicked or has not returned.
	Stop(ctx context.Context) error
}

// orderer is the optional method that places a Runner in the start order.
type orderer interface {
	Order() int
}

// orderOf returns r's Order, or 0 when r has no Order method.
func orderOf(r Runner) int {
	if o, ok := r.(orderer); ok {
		return o.Order()
	}
	return 0
}

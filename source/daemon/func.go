package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNilFunc is the exit of a Runner built by Func with a nil function: it starts, then exits at
// once with this error.
var ErrNilFunc = errors.New("daemon: nil run function")

// errFuncStarted is what Start returns on a Func Runner that has already been started or stopped.
var errFuncStarted = errors.New("daemon: func runner already started or stopped")

// FuncOption configures a Runner built by Func.
type FuncOption interface {
	applyFunc(r *funcRunner)
}

// OrderOption places a Runner in the Supervisor's start order. Build one with WithOrder.
type OrderOption struct {
	order int
}

// WithOrder sets the Runner's Order: the Supervisor starts Runners in ascending Order, and a
// Runner built without it has Order 0.
func WithOrder(order int) OrderOption {
	return OrderOption{order: order}
}

func (o OrderOption) applyFunc(r *funcRunner) { r.order = o.order }

// Func returns a Runner called name whose work is run, called in a goroutine of its own.
//
// Start launches that goroutine and returns at once: the Runner is ready as soon as run is
// called. run gets a context that carries the values of the Start context but not its
// cancellation or deadline, so it keeps running when the start phase ends; Stop cancels it. The
// Runner exits when run returns: Done closes and Err reports what run returned. Stopped by Stop,
// a run that returns ctx.Err() exits with context.Canceled, which the Supervisor takes as a clean
// stop. A panic in run is recovered into a *PanicError, which becomes the Runner's Err and is
// handled under the Supervisor's PanicPolicy.
//
// Func does not validate name: New rejects a blank one, as it does for any Runner. A nil run
// makes a Runner that starts and exits at once with ErrNilFunc.
//
// Start may be called once. A second Start, or a Start after Stop, returns an error and changes
// nothing. Stop before Start returns nil and leaves the Runner never started: Done never closes.
func Func(name string, run func(ctx context.Context) error, opts ...FuncOption) Runner {
	r := &funcRunner{name: name, run: run, done: make(chan struct{})}
	for _, opt := range opts {
		if opt != nil {
			opt.applyFunc(r)
		}
	}
	return r
}

// funcRunner is the Runner Func builds.
type funcRunner struct {
	name  string
	run   func(ctx context.Context) error
	order int

	// done closes once run has returned; err is written before it closes and read only after.
	done chan struct{}
	err  error

	mu sync.Mutex
	// claimed is set by the first Start or Stop; only a Start that claims the Runner launches run.
	claimed bool
	// cancel cancels the context run gets; it is nil until Start launches run.
	cancel context.CancelFunc
}

// Name returns the name given to Func.
func (r *funcRunner) Name() string { return r.name }

// Order returns the Order set by WithOrder, or 0.
func (r *funcRunner) Order() int { return r.order }

// Start launches run in a goroutine and returns nil, or an error when the Runner was already
// started or stopped.
func (r *funcRunner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed {
		return fmt.Errorf("daemon: runner %q: %w", r.name, errFuncStarted)
	}
	r.claimed = true

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	go func() {
		defer cancel()
		var err error
		if r.run == nil {
			err = ErrNilFunc
		} else {
			err = guard(r.name, func() error { return r.run(runCtx) }).err
		}
		r.err = err
		close(r.done)
	}()
	return nil
}

// Done is closed once run has returned.
func (r *funcRunner) Done() <-chan struct{} { return r.done }

// Err returns what run returned, or the *PanicError of its panic, once Done is closed, and nil
// before.
func (r *funcRunner) Err() error {
	select {
	case <-r.done:
		return r.err
	default:
		return nil
	}
}

// Stop cancels the context run gets and waits for run to return. It returns ctx's error when ctx
// is done first, leaving run to finish on its own.
func (r *funcRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.claimed = true
	cancel := r.cancel
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}

	cancel()
	select {
	case <-r.done:
		return nil
	default:
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

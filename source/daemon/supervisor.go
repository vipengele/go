package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// defaultGrace is the grace period when WithGrace is not given.
const defaultGrace = 10 * time.Second

// defaultStartTimeout is the start timeout when WithStartTimeout is not given.
const defaultStartTimeout = 30 * time.Second

// Supervisor starts a fixed set of Runners together and stops them together. Build one with New.
type Supervisor struct {
	// runners is in start order: ascending Order, ties in the order given to New.
	runners      []Runner
	grace        time.Duration
	startTimeout time.Duration
	policy       PanicPolicy

	// ran is set by the first Run; a second Run reports ErrAlreadyRan.
	ran atomic.Bool

	// newStartContext returns the context handed to every Start and the function that releases
	// it. It carries the values and the cancellation of parent and bounds the start phase by the
	// start timeout.
	newStartContext func(parent context.Context) (context.Context, context.CancelFunc)

	// newStopContext returns the context handed to Stop and the function that releases it. It
	// bounds the stop phase by the grace period.
	newStopContext func() (context.Context, context.CancelFunc)

	// waiting is called each time Run starts waiting for shutdown while every Runner is running.
	waiting func()
}

// New returns a Supervisor for runners. It rejects an empty list, a nil Runner, a Runner whose
// name is blank, two Runners with the same name, and an Option that fails. The slice is copied, so
// the caller may reuse it.
func New(runners []Runner, opts ...Option) (*Supervisor, error) {
	if len(runners) == 0 {
		return nil, errors.New("daemon: no runners")
	}

	seen := make(map[string]int, len(runners))
	for i, r := range runners {
		if r == nil {
			return nil, fmt.Errorf("daemon: runner %d is nil", i)
		}
		name := r.Name()
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("daemon: runner %d has a blank name", i)
		}
		if first, dup := seen[name]; dup {
			return nil, fmt.Errorf("daemon: runners %d and %d share the name %q", first, i, name)
		}
		seen[name] = i
	}

	s := &Supervisor{
		runners:      slices.Clone(runners),
		grace:        defaultGrace,
		startTimeout: defaultStartTimeout,
		policy:       PanicFail,
		waiting:      func() {},
	}
	slices.SortStableFunc(s.runners, func(a, b Runner) int { return cmp.Compare(orderOf(a), orderOf(b)) })
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("daemon: nil option")
		}
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	startTimeout, grace := s.startTimeout, s.grace
	s.newStartContext = func(parent context.Context) (context.Context, context.CancelFunc) {
		return context.WithTimeout(parent, startTimeout)
	}
	s.newStopContext = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), grace)
	}
	return s, nil
}

// Run starts every Runner, waits until shutdown begins and blocks until they have all been stopped.
//
// The start phase calls each Runner's Start in start order, ascending Order, and waits for it to
// return before calling the next. Every Start shares one context, which carries the values and the
// cancellation of ctx and is cancelled when the start phase ends. The whole phase is bounded by
// the start timeout. A Start that returns an error or panics is a start failure; a Start still
// running when the timeout expires is ErrStartTimeout naming it; ctx cancelled during the phase
// ends it without a failure. In each case no further Start is called and shutdown begins.
//
// Once every Runner has started, Run waits until ctx is done or a Runner exits, which its Done
// reports. An exit before shutdown is the failure that begins it: ErrRunnerExited when Err is nil,
// otherwise Err naming the Runner. Under PanicIsolate an exit whose Err is a *PanicError is
// recorded instead and Run keeps waiting with the other Runners still running, even once every
// Runner has exited so.
//
// A panic in Start, Stop or Err is recovered in the goroutine that called it and becomes a
// *PanicError naming the Runner; it is never re-raised. A panic in Start is a start failure under
// either policy, and a panic in Stop is a stop problem.
//
// The whole shutdown shares one grace period. Each Runner whose Start was called is stopped in
// turn, in reverse start order: Run calls its Stop exactly once and waits for Stop to return and
// then for Done to close, or, for a Runner whose Start has not returned, for Start to return. A
// Stop that returns is never taken as the Runner having exited. A Stop that returns an error, or
// a Stop, Done or Start still pending when the grace period ends, is reported as ErrStopTimeout
// naming the Runner, and a Stop that panics as its *PanicError; the Runners after it are still
// stopped, but once the grace period has ended Run no longer waits for them. An abandoned Stop or
// Start may still be running when Run returns.
//
// Run returns nil when ctx is cancelled and every Runner stops cleanly. Otherwise it returns,
// joined with errors.Join, the failure that began shutdown first, then each panic exit isolated
// under PanicIsolate in start order, then each stop problem in stop order. A Runner's exit during
// shutdown with an Err wrapping context.Canceled is not a problem and is dropped; any other Err is
// a stop problem.
//
// Run may be called once; a second call returns ErrAlreadyRan.
func (s *Supervisor) Run(ctx context.Context) error {
	if !s.ran.CompareAndSwap(false, true) {
		return ErrAlreadyRan
	}

	runs, failure, complete := s.start(ctx)
	if complete {
		failure = s.wait(ctx, runs)
	}

	stopCtx, cancel := s.newStopContext()
	defer cancel()

	errs := []error{failure}
	for _, l := range runs {
		errs = append(errs, l.isolated)
	}
	for _, l := range slices.Backward(runs) {
		errs = append(errs, l.stop(stopCtx)...)
	}
	return errors.Join(errs...)
}

// start runs the start phase. It returns every Runner whose Start it called, in start order, the
// failure that ends the phase early, and whether every Runner started.
func (s *Supervisor) start(ctx context.Context) (runs []*launched, failure error, complete bool) {
	startCtx, cancel := s.newStartContext(ctx)
	defer cancel()

	runs = make([]*launched, 0, len(s.runners))
	for _, r := range s.runners {
		if startCtx.Err() != nil {
			return runs, interrupted(ctx, ""), false
		}
		l := &launched{runner: r, name: r.Name(), state: starting, result: make(chan outcome, 1)}
		runs = append(runs, l)
		go func() { l.result <- guard(l.name, func() error { return r.Start(startCtx) }) }()

		select {
		case o := <-l.result:
			switch {
			case o.err == nil:
				l.running()
			case !o.isPanic() && startCtx.Err() != nil && isContextErr(o.err):
				l.state = failed
				return runs, interrupted(ctx, l.name), false
			default:
				l.state = failed
				return runs, l.wrap(o), false
			}
		case <-startCtx.Done():
			return runs, interrupted(ctx, l.name), false
		}
	}
	return runs, nil, true
}

// interrupted returns what ends a start phase whose context is done: nil when ctx, the context
// passed to Run, is done, and ErrStartTimeout otherwise, naming the Runner whose Start was running
// when name is not empty.
func interrupted(ctx context.Context, name string) error {
	switch {
	case ctx.Err() != nil:
		return nil
	case name == "":
		return fmt.Errorf("daemon: %w", ErrStartTimeout)
	default:
		return fmt.Errorf("daemon: runner %q: %w", name, ErrStartTimeout)
	}
}

// isContextErr reports whether err only reports the end of a context.
func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// wait blocks until shutdown begins: ctx is done, or a running Runner exits in a way that fails
// it. It returns that failure, or nil when ctx began it. A panic exit under PanicIsolate is
// recorded on its Runner and does not end the wait.
func (s *Supervisor) wait(ctx context.Context, runs []*launched) error {
	quit := make(chan struct{})
	defer close(quit)
	exited := make(chan *launched, len(runs))
	for _, l := range runs {
		go func() {
			select {
			case <-l.done:
				exited <- l
			case <-quit:
			}
		}()
	}

	for {
		s.waiting()
		select {
		case <-ctx.Done():
			return nil
		case l := <-exited:
			l.exited = true
			o := guard(l.name, l.runner.Err)
			if s.policy == PanicIsolate && o.isPanic() {
				l.isolated = o.err
				continue
			}
			if o.err == nil {
				return fmt.Errorf("daemon: runner %q: %w", l.name, ErrRunnerExited)
			}
			return l.wrap(o)
		}
	}
}

// outcome is what a Runner's Start, Stop or Err came to: the error it returned, or, with panicked
// set, the *PanicError recovered from it.
type outcome struct {
	err      error
	panicked bool
}

// isPanic reports whether the outcome is a recovered panic or an error carrying a *PanicError.
func (o outcome) isPanic() bool {
	var pe *PanicError
	return o.panicked || errors.As(o.err, &pe)
}

// guard calls f, a method of the Runner called name, and returns its outcome. A panic in f is
// recovered into a *PanicError carrying the panic value and the stack of the panicking goroutine,
// so guard must run in the goroutine that calls f.
func guard(name string, f func() error) (o outcome) {
	defer func() {
		if v := recover(); v != nil {
			o = outcome{err: &PanicError{Runner: name, Value: v, Stack: debug.Stack()}, panicked: true}
		}
	}()
	return outcome{err: f()}
}

// startState is how far a launched Runner's Start got.
type startState int

const (
	// starting is a Start that has not returned.
	starting startState = iota
	// running is a Start that returned nil: the Runner is running until Done closes.
	running
	// failed is a Start that returned an error or panicked: the Runner never ran.
	failed
)

// launched is one Runner whose Start Run has called. Its fields other than result are read and
// written only by the goroutine running Run.
type launched struct {
	runner Runner
	name   string
	state  startState

	// result receives what Start came to. Its capacity of one means the Start goroutine never
	// blocks on it, even once Run has stopped listening.
	result chan outcome

	// done is the Runner's Done, set once it is running.
	done <-chan struct{}

	// exited records that Run has seen done close before shutdown and dealt with the exit.
	exited bool

	// isolated is the *PanicError of a panic exit before shutdown under PanicIsolate.
	isolated error
}

// running records that the Runner's Start returned nil.
func (l *launched) running() {
	l.state = running
	l.done = l.runner.Done()
}

// wrap names the Runner in an error outcome. A panic, which already names the Runner, is returned
// as it is.
func (l *launched) wrap(o outcome) error {
	if o.isPanic() {
		return o.err
	}
	return fmt.Errorf("daemon: runner %q: %w", l.name, o.err)
}

// stop calls the Runner's Stop and waits, under ctx, for Stop to return and then for the Runner to
// finish: Done to close for a running Runner, Start to return for one still starting. It reports
// what went wrong, at most one problem with Stop per Runner: a Stop error as ErrStopTimeout with
// its cause, a Stop panic as its *PanicError, or a Stop or the Runner still pending when ctx is
// done as ErrStopTimeout; and the Runner's exit or late Start problem as itself. Once ctx is done
// it still calls Stop, but reports a timeout without waiting.
func (l *launched) stop(ctx context.Context) []error {
	stopped := make(chan outcome, 1)
	go func() { stopped <- guard(l.name, func() error { return l.runner.Stop(ctx) }) }()

	if ctx.Err() != nil {
		return []error{l.timeout()}
	}
	select {
	case o := <-stopped:
		switch {
		case o.panicked:
			return append([]error{o.err}, l.finish(ctx, false)...)
		case o.err != nil:
			problem := fmt.Errorf("daemon: runner %q: %w: %w", l.name, ErrStopTimeout, o.err)
			return append([]error{problem}, l.finish(ctx, false)...)
		default:
			return l.finish(ctx, true)
		}
	case <-ctx.Done():
		return []error{l.timeout()}
	}
}

// finish waits, under ctx, for a stopped Runner to finish and reports its problem: an exit Err
// other than the cancellation it was asked for, or a Start that returned late with an error other
// than the end of its context or panicked. A Runner whose late Start returned nil is waited for
// as a running one. A Runner still pending when ctx is done is reported as a timeout only when
// reportTimeout is set.
func (l *launched) finish(ctx context.Context, reportTimeout bool) []error {
	if l.state == starting {
		select {
		case o := <-l.result:
			if o.err != nil {
				l.state = failed
				if !o.isPanic() && isContextErr(o.err) {
					return nil
				}
				return []error{l.wrap(o)}
			}
			l.running()
		case <-ctx.Done():
			return l.pending(reportTimeout)
		}
	}
	if l.state != running || l.exited {
		return nil
	}
	select {
	case <-l.done:
		o := guard(l.name, l.runner.Err)
		if o.err == nil || (!o.isPanic() && errors.Is(o.err, context.Canceled)) {
			return nil
		}
		return []error{l.wrap(o)}
	case <-ctx.Done():
		return l.pending(reportTimeout)
	}
}

// pending reports a Runner still pending when the grace period ended, when reportTimeout is set.
func (l *launched) pending(reportTimeout bool) []error {
	if reportTimeout {
		return []error{l.timeout()}
	}
	return nil
}

// timeout reports that the Runner did not finish stopping within the grace period.
func (l *launched) timeout() error {
	return fmt.Errorf("daemon: runner %q: %w", l.name, ErrStopTimeout)
}

package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// defaultGrace is the grace period when WithGrace is not given.
const defaultGrace = 10 * time.Second

// Supervisor starts a fixed set of Runners together and stops them together. Build one with New.
type Supervisor struct {
	runners []Runner
	grace   time.Duration
	policy  PanicPolicy

	// ran is set by the first Run; a second Run reports ErrAlreadyRan.
	ran atomic.Bool

	// newStopContext returns the context handed to Stop and the function that releases it. It
	// bounds the stop phase by the grace period.
	newStopContext func() (context.Context, context.CancelFunc)
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
		runners: slices.Clone(runners),
		grace:   defaultGrace,
		policy:  PanicFail,
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("daemon: nil option")
		}
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	grace := s.grace
	s.newStopContext = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), grace)
	}
	return s, nil
}

// Run starts every Runner and blocks until they have all been stopped.
//
// Each Runner's Start runs in its own goroutine, launched in the order given to New without
// waiting for one to be ready before the next. A Start receives a context of its own that carries
// the values of ctx but not its cancellation: Run watches ctx and, once it is done or once any
// Start returns, shuts the Runners down in reverse start order. A Start that returns before
// shutdown, with nil or an error, is the failure that begins it.
//
// The whole shutdown shares one deadline, the grace period. For each Runner in turn, Run cancels
// its Start context, calls its Stop and waits for Stop and then Start to return. Every Runner's
// Stop is called exactly once, including the one whose Start failed. A Stop that returns an error,
// or a Stop or Start still running at the deadline, is reported as ErrStopTimeout naming the
// Runner; the Runners after it are still cancelled and their Stop called, but once the deadline
// has passed Run no longer waits for them. An abandoned Stop or Start goroutine may still be
// running when Run returns.
//
// Run returns nil when ctx is cancelled and every Runner stops cleanly. Otherwise it returns the
// failure that began shutdown first, then each stop problem in stop order, joined with
// errors.Join. An error from a Start that only reports the cancellation of its context during
// shutdown is not a problem and is dropped.
//
// Run may be called once; a second call returns ErrAlreadyRan.
func (s *Supervisor) Run(ctx context.Context) error {
	if !s.ran.CompareAndSwap(false, true) {
		return ErrAlreadyRan
	}

	exited := make(chan *launched, len(s.runners))
	runs := make([]*launched, 0, len(s.runners))
	for _, r := range s.runners {
		runs = append(runs, launch(ctx, r, exited))
	}

	var failure error
	select {
	case <-ctx.Done():
	case l := <-exited:
		failure = l.failure(<-l.result)
	}

	stopCtx, cancel := s.newStopContext()
	defer cancel()

	errs := []error{failure}
	for _, l := range slices.Backward(runs) {
		errs = append(errs, l.stop(stopCtx)...)
	}
	return errors.Join(errs...)
}

// launched is one Runner whose Start is running under Run. Its fields other than result are
// read and written only by the goroutine running Run.
type launched struct {
	runner Runner
	name   string
	cancel context.CancelFunc

	// result receives what Start returned. Its capacity of one means the Start goroutine never
	// blocks on it, even once Run has stopped listening.
	result chan error

	// returned records that Run has received Start's result.
	returned bool
}

// launch runs r's Start in a goroutine under a context that keeps the values of parent but not
// its cancellation. When Start returns, its result goes to the launched Runner's result channel
// and the launched Runner itself to exited, which must have room for every Runner.
func launch(parent context.Context, r Runner, exited chan<- *launched) *launched {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	l := &launched{runner: r, name: r.Name(), cancel: cancel, result: make(chan error, 1)}
	go func() {
		l.result <- r.Start(ctx)
		exited <- l
	}()
	return l
}

// failure turns the result of a Start that returned before shutdown into the error that begins
// it. The Runner's context is not cancelled at that point, so every result is a failure.
func (l *launched) failure(err error) error {
	l.returned = true
	if err == nil {
		return fmt.Errorf("daemon: runner %q: %w", l.name, ErrRunnerExited)
	}
	return fmt.Errorf("daemon: runner %q: %w", l.name, err)
}

// stop cancels the Runner's Start context, calls its Stop and waits, under ctx, for Stop and then
// Start to return. It reports what went wrong, at most one ErrStopTimeout per Runner: a Stop error
// with its cause, or a Stop or Start still running when ctx is done; and a Start error other than
// the cancellation it was asked for, as itself. Once ctx is done it still cancels and calls Stop,
// but reports a timeout without waiting.
func (l *launched) stop(ctx context.Context) []error {
	l.cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- l.runner.Stop(ctx) }()

	if ctx.Err() != nil {
		return []error{l.timeout()}
	}
	select {
	case err := <-stopped:
		if err != nil {
			return append([]error{fmt.Errorf("daemon: runner %q: %w: %w", l.name, ErrStopTimeout, err)},
				l.waitStart(ctx, false)...)
		}
	case <-ctx.Done():
		return []error{l.timeout()}
	}
	return l.waitStart(ctx, true)
}

// waitStart waits, under ctx, for the Runner's Start to return after its context was cancelled.
// A Start still running when ctx is done is reported as a timeout only when reportTimeout is set.
func (l *launched) waitStart(ctx context.Context, reportTimeout bool) []error {
	if l.returned {
		return nil
	}
	select {
	case err := <-l.result:
		l.returned = true
		if err != nil && !errors.Is(err, context.Canceled) {
			return []error{fmt.Errorf("daemon: runner %q: %w", l.name, err)}
		}
	case <-ctx.Done():
		if reportTimeout {
			return []error{l.timeout()}
		}
	}
	return nil
}

// timeout reports that the Runner did not finish stopping before the shutdown deadline.
func (l *launched) timeout() error {
	return fmt.Errorf("daemon: runner %q: %w", l.name, ErrStopTimeout)
}

package daemon

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRunner is a Runner that has exited as soon as it starts.
type fakeRunner struct{ name string }

func (f *fakeRunner) Name() string                { return f.name }
func (f *fakeRunner) Start(context.Context) error { return nil }
func (f *fakeRunner) Err() error                  { return nil }
func (f *fakeRunner) Stop(context.Context) error  { return nil }

func (f *fakeRunner) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func TestNewRejects(t *testing.T) {
	tests := []struct {
		name    string
		runners []Runner
		opts    []Option
	}{
		{name: "nil list"},
		{name: "empty list", runners: []Runner{}},
		{name: "nil runner", runners: []Runner{&fakeRunner{"a"}, nil}},
		{name: "empty name", runners: []Runner{&fakeRunner{""}}},
		{name: "whitespace name", runners: []Runner{&fakeRunner{" \t\n"}}},
		{name: "duplicate names", runners: []Runner{&fakeRunner{"a"}, &fakeRunner{"b"}, &fakeRunner{"a"}}},
		{name: "zero grace", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithGrace(0)}},
		{name: "negative grace", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithGrace(-time.Second)}},
		{name: "zero start timeout", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithStartTimeout(0)}},
		{name: "negative start timeout", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithStartTimeout(-time.Second)}},
		{name: "unknown policy", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithPanicPolicy(PanicPolicy(99))}},
		{name: "negative policy", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithPanicPolicy(PanicPolicy(-1))}},
		{name: "nil option", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.runners, tt.opts...)
			if err == nil {
				t.Fatalf("New succeeded, want an error")
			}
			if s != nil {
				t.Errorf("New returned a Supervisor alongside an error")
			}
		})
	}
}

func TestNewErrorNamesTheOffender(t *testing.T) {
	_, err := New([]Runner{&fakeRunner{"a"}, &fakeRunner{"a"}})
	if err == nil || !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("duplicate-name error = %v, want it to name %q", err, "a")
	}
	_, err = New([]Runner{&fakeRunner{"a"}, nil})
	if err == nil || !strings.Contains(err.Error(), "1") {
		t.Errorf("nil-runner error = %v, want it to name index 1", err)
	}
}

func TestNewAccepts(t *testing.T) {
	tests := []struct {
		name       string
		runners    []Runner
		opts       []Option
		wantLen    int
		wantGrace  time.Duration
		wantPolicy PanicPolicy
	}{
		{
			name:       "one runner, defaults",
			runners:    []Runner{&fakeRunner{"a"}},
			wantLen:    1,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "several runners",
			runners:    []Runner{&fakeRunner{"a"}, &fakeRunner{"b"}, &fakeRunner{"c"}},
			wantLen:    3,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "names differing in case or padding",
			runners:    []Runner{&fakeRunner{"a"}, &fakeRunner{"A"}, &fakeRunner{" a"}},
			wantLen:    3,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "custom grace",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithGrace(3 * time.Second)},
			wantLen:    1,
			wantGrace:  3 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "isolate policy",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithPanicPolicy(PanicIsolate)},
			wantLen:    1,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicIsolate,
		},
		{
			name:       "explicit fail policy",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithPanicPolicy(PanicFail)},
			wantLen:    1,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "later option wins",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithGrace(time.Second), WithGrace(2 * time.Second)},
			wantLen:    1,
			wantGrace:  2 * time.Second,
			wantPolicy: PanicFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.runners, tt.opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if len(s.runners) != tt.wantLen {
				t.Errorf("len(runners) = %d, want %d", len(s.runners), tt.wantLen)
			}
			if s.grace != tt.wantGrace {
				t.Errorf("grace = %v, want %v", s.grace, tt.wantGrace)
			}
			if s.policy != tt.wantPolicy {
				t.Errorf("policy = %v, want %v", s.policy, tt.wantPolicy)
			}
			if s.ran.Load() {
				t.Errorf("a new Supervisor reports it already ran")
			}
		})
	}
}

func TestPanicFailIsTheZeroPolicy(t *testing.T) {
	var p PanicPolicy
	if p != PanicFail {
		t.Errorf("zero PanicPolicy = %d, want PanicFail", p)
	}
}

func TestNewCopiesTheRunnerSlice(t *testing.T) {
	a, b := &fakeRunner{"a"}, &fakeRunner{"b"}
	runners := []Runner{a, b}
	s, err := New(runners)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runners[0] = nil
	runners[1] = &fakeRunner{"z"}
	if s.runners[0] != Runner(a) || s.runners[1] != Runner(b) {
		t.Errorf("Supervisor runners changed with the caller's slice")
	}
}

func TestDefaultStopContextIsBoundedByGrace(t *testing.T) {
	const grace = time.Minute
	s, err := New([]Runner{&fakeRunner{"a"}}, WithGrace(grace))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := s.newStopContext()
	defer cancel()

	if err := ctx.Err(); err != nil {
		t.Fatalf("stop context already done: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("stop context has no deadline")
	}
	if time.Until(deadline) > grace {
		t.Errorf("deadline is %v away, want at most %v", time.Until(deadline), grace)
	}
}

func TestStartContextIsBoundedByTheStartTimeoutAndFollowsTheParent(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want time.Duration
	}{
		{name: "default", want: 30 * time.Second},
		{name: "custom", opts: []Option{WithStartTimeout(time.Minute)}, want: time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New([]Runner{&fakeRunner{"a"}}, tt.opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if s.startTimeout != tt.want {
				t.Errorf("startTimeout = %v, want %v", s.startTimeout, tt.want)
			}
			parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "run"))
			ctx, cancel := s.newStartContext(parent)
			defer cancel()

			if err := ctx.Err(); err != nil {
				t.Fatalf("start context already done: %v", err)
			}
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatalf("start context has no deadline")
			}
			if time.Until(deadline) > tt.want {
				t.Errorf("deadline is %v away, want at most %v", time.Until(deadline), tt.want)
			}
			if got := ctx.Value(ctxKey{}); got != "run" {
				t.Errorf("start context carries %v, want the parent's value", got)
			}
			cancelParent()
			if err := ctx.Err(); !errors.Is(err, context.Canceled) {
				t.Errorf("start context after the parent is cancelled = %v, want cancelled", err)
			}
		})
	}
}

// recorder is the ordered log of Start and Stop calls shared by the scripted Runners of one test.
type recorder struct {
	mu     sync.Mutex
	starts []string
	stops  []string
}

func (r *recorder) started(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, name)
}

func (r *recorder) stopped(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops = append(r.stops, name)
}

func (r *recorder) startOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.starts)
}

func (r *recorder) stopOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.stops)
}

// scriptedRunner is a Runner driven by its test. Start reports its context on started and returns
// nil, startErr, or panics with startPanicValue when startPanics is set; with hangStart it blocks
// until its context is done, returning the context's error, or until the test frees it, returning
// nil. The test ends a running Runner with exit or panicExit. Stop reports itself on stopCalled,
// ends the Runner with stopExit (by default an error wrapping context.Canceled), or with a panic
// exit of stopExitPanic, and returns stopErr; with keepRunning it leaves the Runner running, with
// hangStop it blocks until the test frees it, and with stopPanic it panics.
type scriptedRunner struct {
	name string
	rec  *recorder

	started    chan context.Context
	stopCalled chan struct{}
	release    chan struct{}
	freeOnce   sync.Once
	done       chan struct{}
	exitOnce   sync.Once

	mu  sync.Mutex
	err error

	startErr        error
	startPanics     bool
	startPanicValue any
	hangStart       bool

	stopErr       error
	stopPanic     any
	hangStop      bool
	stopExit      error
	stopExitPanic any
	keepRunning   bool

	// onStop, when set, runs inside Stop before it returns.
	onStop func()
}

func newScripted(name string) *scriptedRunner {
	return &scriptedRunner{
		name:       name,
		started:    make(chan context.Context, 1),
		stopCalled: make(chan struct{}, 1),
		release:    make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func (r *scriptedRunner) Name() string { return r.name }

func (r *scriptedRunner) Start(ctx context.Context) error {
	r.rec.started(r.name)
	r.started <- ctx
	switch {
	case r.startPanics:
		panic(r.startPanicValue)
	case r.hangStart:
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.release:
			return nil
		}
	}
	return r.startErr
}

func (r *scriptedRunner) Done() <-chan struct{} { return r.done }

func (r *scriptedRunner) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *scriptedRunner) Stop(context.Context) error {
	r.rec.stopped(r.name)
	r.stopCalled <- struct{}{}
	if !r.keepRunning {
		defer r.endByStop()
	}
	if r.onStop != nil {
		r.onStop()
	}
	if r.stopPanic != nil {
		panic(r.stopPanic)
	}
	if r.hangStop {
		<-r.release
	}
	return r.stopErr
}

// endByStop ends the Runner the way Stop has it exit.
func (r *scriptedRunner) endByStop() {
	switch {
	case r.stopExitPanic != nil:
		r.panicExit(r.stopExitPanic)
	case r.stopExit != nil:
		r.exit(r.stopExit)
	default:
		r.exit(fmt.Errorf("%s: %w", r.name, context.Canceled))
	}
}

// exit ends the Runner with err, once.
func (r *scriptedRunner) exit(err error) {
	r.exitOnce.Do(func() {
		r.mu.Lock()
		r.err = err
		r.mu.Unlock()
		close(r.done)
	})
}

// panicExit ends the Runner with the *PanicError of a panic with v, recovered as a Runner
// recovers a panic in its own work.
func (r *scriptedRunner) panicExit(v any) {
	defer func() { r.exit(&PanicError{Runner: r.name, Value: recover(), Stack: debug.Stack()}) }()
	panic(v)
}

// free releases a hung Start or Stop.
func (r *scriptedRunner) free() {
	r.freeOnce.Do(func() { close(r.release) })
}

// orderedRunner is a scripted Runner with an Order.
type orderedRunner struct {
	*scriptedRunner
	order int
}

func (r orderedRunner) Order() int { return r.order }

// ctxKey keys the value the run context carries, to check that the Start context inherits it.
type ctxKey struct{}

// harness runs a Supervisor over scripted Runners. Its start and stop contexts have no deadline:
// the test ends the start phase by calling expireStart and the grace period by calling cancelStop.
type harness struct {
	t           *testing.T
	baseline    int
	rec         *recorder
	runners     []*scriptedRunner
	startCtx    []context.Context
	cancelRun   context.CancelFunc
	expireStart context.CancelFunc
	cancelStop  context.CancelFunc
	result      chan error

	// waiting receives a value each time Run starts waiting for shutdown with every Runner running.
	waiting chan struct{}
}

// launchRun starts Run over list, whose Runners are scripted or ordered scripted ones, and returns
// at once.
func launchRun(t *testing.T, opts []Option, list ...Runner) *harness {
	t.Helper()
	h := &harness{t: t, baseline: runtime.NumGoroutine(), rec: &recorder{}}
	for _, r := range list {
		var sr *scriptedRunner
		switch r := r.(type) {
		case *scriptedRunner:
			sr = r
		case orderedRunner:
			sr = r.scriptedRunner
		default:
			t.Fatalf("launchRun: unexpected runner %T", r)
		}
		sr.rec = h.rec
		h.runners = append(h.runners, sr)
	}
	s, err := New(list, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	expire, expireStart := context.WithCancel(context.Background())
	s.newStartContext = func(parent context.Context) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		stop := context.AfterFunc(expire, cancel)
		return ctx, func() { stop(); cancel() }
	}
	stopCtx, cancelStop := context.WithCancel(context.Background())
	s.newStopContext = func() (context.Context, context.CancelFunc) { return stopCtx, cancelStop }
	h.waiting = make(chan struct{}, 16)
	s.waiting = func() {
		select {
		case h.waiting <- struct{}{}:
		default:
		}
	}
	runCtx, cancelRun := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "run"))
	h.cancelRun, h.expireStart, h.cancelStop = cancelRun, expireStart, cancelStop

	h.result = make(chan error, 1)
	go func() { h.result <- s.Run(runCtx) }()
	return h
}

// runScripted starts Run over runners and returns once every Runner has started and Run is
// waiting for shutdown.
func runScripted(t *testing.T, runners ...*scriptedRunner) *harness {
	t.Helper()
	return runScriptedWith(t, nil, runners...)
}

// runScriptedWith is runScripted with options for New.
func runScriptedWith(t *testing.T, opts []Option, runners ...*scriptedRunner) *harness {
	t.Helper()
	list := make([]Runner, len(runners))
	for i, r := range runners {
		list[i] = r
	}
	h := launchRun(t, opts, list...)
	for _, r := range runners {
		h.startCtx = append(h.startCtx, <-r.started)
	}
	<-h.waiting
	return h
}

// finish releases every hung call and checks that no goroutine of the run outlives it.
func (h *harness) finish() {
	h.t.Helper()
	for _, r := range h.runners {
		r.free()
	}
	h.cancelRun()
	h.expireStart()
	h.cancelStop()
	settle(h.t, h.baseline)
}

// settle yields until the goroutine count is back to baseline, and fails the test when a bounded
// number of yields does not get it there.
func settle(t *testing.T, baseline int) {
	t.Helper()
	for range 1_000_000 {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		runtime.Gosched()
	}
	t.Errorf("%d goroutines remain, want at most %d", runtime.NumGoroutine(), baseline)
}

// joined returns the errors Run joined, in order.
func joined(t *testing.T, err error) []error {
	t.Helper()
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("Run returned %v (%T), want an errors.Join result", err, err)
	}
	return j.Unwrap()
}

func checkOrder(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s order = %v, want %v", what, got, want)
	}
}

func TestRunStopsInReverseOrderAndReturnsNilOnParentCancel(t *testing.T) {
	h := runScripted(t, newScripted("a"), newScripted("b"), newScripted("c"))
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkOrder(t, "start", h.rec.startOrder(), []string{"a", "b", "c"})
	checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
	h.finish()
}

func TestRunStartsInAscendingOrderAwaitingEachAndStopsInReverse(t *testing.T) {
	a, b, c, d := newScripted("a"), newScripted("b"), newScripted("c"), newScripted("d")
	c.hangStart = true
	h := launchRun(t, nil,
		orderedRunner{a, 1}, b, orderedRunner{c, -1}, orderedRunner{d, 1})

	<-c.started
	if got := h.rec.startOrder(); len(got) != 1 {
		t.Errorf("Start calls while c's Start runs = %v, want only c", got)
	}
	c.free()
	<-h.waiting
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkOrder(t, "start", h.rec.startOrder(), []string{"c", "b", "a", "d"})
	checkOrder(t, "stop", h.rec.stopOrder(), []string{"d", "a", "b", "c"})
	h.finish()
}

func TestRunExitBeforeShutdownStopsEveryRunnerAndIsReturnedFirst(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		failing int
		exitErr error
		want    error
	}{
		{name: "exit error from the first runner", failing: 0, exitErr: boom, want: boom},
		{name: "exit error from the last runner", failing: 2, exitErr: boom, want: boom},
		{name: "exit with nil from the middle runner", failing: 1, want: ErrRunnerExited},
		{name: "exit with nil from the first runner", failing: 0, want: ErrRunnerExited},
		{name: "exit with cancellation", failing: 1, exitErr: context.Canceled, want: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := runScripted(t, newScripted("a"), newScripted("b"), newScripted("c"))
			failing := h.runners[tt.failing]
			failing.exit(tt.exitErr)

			err := <-h.result
			errs := joined(t, err)
			if len(errs) != 1 {
				t.Errorf("Run joined %d errors (%v), want only the failure", len(errs), err)
			}
			if !errors.Is(errs[0], tt.want) {
				t.Errorf("first error = %v, want it to wrap %v", errs[0], tt.want)
			}
			if !strings.Contains(errs[0].Error(), fmt.Sprintf("%q", failing.name)) {
				t.Errorf("first error = %v, want it to name %q", errs[0], failing.name)
			}
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			h.finish()
		})
	}
}

func TestRunDropsExitErrorsThatReportCancellation(t *testing.T) {
	a, b := newScripted("a"), newScripted("b")
	b.stopExit = fmt.Errorf("serve: %w", context.Canceled)
	h := runScripted(t, a, b)
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	h.finish()
}

func TestRunTwiceReturnsErrAlreadyRan(t *testing.T) {
	baseline := runtime.NumGoroutine()
	s, err := New([]Runner{&fakeRunner{"a"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Run(context.Background()); !errors.Is(err, ErrRunnerExited) {
		t.Fatalf("first Run = %v, want %v", err, ErrRunnerExited)
	}
	if err := s.Run(context.Background()); !errors.Is(err, ErrAlreadyRan) {
		t.Errorf("second Run = %v, want %v", err, ErrAlreadyRan)
	}
	settle(t, baseline)
}

func TestRunWithADoneContextStartsNothing(t *testing.T) {
	a := newScripted("a")
	a.rec = &recorder{}
	s, err := New([]Runner{a})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Run(ctx); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if got := a.rec.startOrder(); len(got) != 0 {
		t.Errorf("Start called on %v, want none", got)
	}
	if got := a.rec.stopOrder(); len(got) != 0 {
		t.Errorf("Stop called on %v, want none", got)
	}
}

func TestRunStartFailureStopsTheStartedRunnersInReverse(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name   string
		policy PanicPolicy
		// setup makes b's Start fail.
		setup func(b *scriptedRunner)
		// panicValue is what b's Start panics with, when panics is set.
		panics     bool
		panicValue any
	}{
		{name: "start error", setup: func(b *scriptedRunner) { b.startErr = boom }},
		{name: "start panic with an error under fail", panics: true, panicValue: boom},
		{name: "start panic with a string under fail", panics: true, panicValue: "oops"},
		{name: "start panic with nil under fail", panics: true},
		{name: "start panic under isolate", policy: PanicIsolate, panics: true, panicValue: "oops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
			if tt.setup != nil {
				tt.setup(b)
			}
			b.startPanics, b.startPanicValue = tt.panics, tt.panicValue
			h := launchRun(t, []Option{WithPanicPolicy(tt.policy)}, a, b, c)

			err := <-h.result
			if tt.panics {
				checkJoined(t, err, []wantErr{{panicked: "b"}})
				checkPanic(t, err, "b", tt.panicValue, "Start")
			} else {
				checkJoined(t, err, []wantErr{{is: boom}})
				if !strings.Contains(err.Error(), `"b"`) {
					t.Errorf("Run = %v, want it to name %q", err, "b")
				}
			}
			checkOrder(t, "start", h.rec.startOrder(), []string{"a", "b"})
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"b", "a"})
			h.finish()
		})
	}
}

func TestRunStartTimeoutStopsTheStartedRunnersInReverse(t *testing.T) {
	a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
	b.hangStart = true
	h := launchRun(t, nil, a, b, c)
	<-a.started
	<-b.started
	h.expireStart()

	err := <-h.result
	checkJoined(t, err, []wantErr{{is: ErrStartTimeout}})
	if !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("Run = %v, want it to name %q", err, "b")
	}
	checkOrder(t, "start", h.rec.startOrder(), []string{"a", "b"})
	checkOrder(t, "stop", h.rec.stopOrder(), []string{"b", "a"})
	h.finish()
}

func TestRunParentCancelDuringStartStopsTheStartedRunnersAndReturnsNil(t *testing.T) {
	a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
	b.hangStart = true
	h := launchRun(t, nil, a, b, c)
	<-a.started
	<-b.started
	h.cancelRun()

	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkOrder(t, "start", h.rec.startOrder(), []string{"a", "b"})
	checkOrder(t, "stop", h.rec.stopOrder(), []string{"b", "a"})
	h.finish()
}

func TestRunReportsAStopProblemAndStillStopsTheRest(t *testing.T) {
	stopBoom := errors.New("stop boom")
	tests := []struct {
		name string
		// setup configures runner b, which is stopped second of three.
		setup func(b *scriptedRunner)
		// hangs reports that b stays pending until the grace period ends.
		hangs bool
		want  []error
	}{
		{
			name:  "stop returns an error",
			setup: func(b *scriptedRunner) { b.stopErr = stopBoom },
			want:  []error{ErrStopTimeout, stopBoom},
		},
		{
			name:  "stop hangs past the deadline",
			setup: func(b *scriptedRunner) { b.hangStop = true },
			hangs: true,
			want:  []error{ErrStopTimeout},
		},
		{
			name:  "done stays open past the deadline after stop returns",
			setup: func(b *scriptedRunner) { b.keepRunning = true },
			hangs: true,
			want:  []error{ErrStopTimeout},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
			tt.setup(b)
			h := runScripted(t, a, b, c)
			h.cancelRun()
			if tt.hangs {
				<-b.stopCalled
				h.cancelStop()
			}

			errs := joined(t, <-h.result)
			if tt.hangs {
				// Past the deadline, a's Stop is called in a goroutine Run does not wait for.
				<-a.stopCalled
			}
			bErr := errs[0]
			for _, want := range tt.want {
				if !errors.Is(bErr, want) {
					t.Errorf("first error = %v, want it to wrap %v", bErr, want)
				}
			}
			if !strings.Contains(bErr.Error(), `"b"`) {
				t.Errorf("first error = %v, want it to name %q", bErr, "b")
			}
			for _, err := range errs {
				if strings.Contains(err.Error(), `"c"`) {
					t.Errorf("Run reported %v for c, which stopped before b", err)
				}
			}
			// Once the deadline has passed, a is stopped without being waited for.
			wantLen := 1
			if tt.hangs {
				wantLen = 2
			}
			if len(errs) != wantLen {
				t.Errorf("Run joined %d errors (%v), want %d", len(errs), errs, wantLen)
			}
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			h.finish()
		})
	}
}

func TestRunStartContextCarriesTheRunValuesAndEndsWithTheStartPhase(t *testing.T) {
	h := runScripted(t, newScripted("a"), newScripted("b"))
	for i, ctx := range h.startCtx {
		if got := ctx.Value(ctxKey{}); got != "run" {
			t.Errorf("Start context %d carries %v, want the run context's value", i, got)
		}
		if err := ctx.Err(); !errors.Is(err, context.Canceled) {
			t.Errorf("Start context %d once every Runner started = %v, want cancelled", i, err)
		}
	}
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	h.finish()
}

func TestRunStopsEachRunnerOnlyAfterTheLaterOnesHaveExited(t *testing.T) {
	a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
	var exitedWhenBStops, exitedWhenAStops []bool
	isDone := func(r *scriptedRunner) bool {
		select {
		case <-r.done:
			return true
		default:
			return false
		}
	}
	b.onStop = func() { exitedWhenBStops = []bool{isDone(a), isDone(c)} }
	a.onStop = func() { exitedWhenAStops = []bool{isDone(b)} }
	h := runScripted(t, a, b, c)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if !slices.Equal(exitedWhenBStops, []bool{false, true}) {
		t.Errorf("a and c exited when b stops = %v, want a running and c exited", exitedWhenBStops)
	}
	if !slices.Equal(exitedWhenAStops, []bool{true}) {
		t.Errorf("b exited when a stops = %v, want exited", exitedWhenAStops)
	}
	h.finish()
}

func TestRunJoinsTheFailureFirstThenStopProblemsInStopOrder(t *testing.T) {
	exitBoom, stopBoom, flushBoom := errors.New("exit boom"), errors.New("stop boom"), errors.New("flush boom")
	a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
	b.stopExit = flushBoom
	c.stopErr = stopBoom
	h := runScripted(t, a, b, c)
	a.exit(exitBoom)

	errs := joined(t, <-h.result)
	want := []error{exitBoom, stopBoom, flushBoom}
	if len(errs) != len(want) {
		t.Fatalf("Run joined %d errors (%v), want %d", len(errs), errs, len(want))
	}
	for i, w := range want {
		if !errors.Is(errs[i], w) {
			t.Errorf("error %d = %v, want it to wrap %v", i, errs[i], w)
		}
	}
	h.finish()
}

// wantErr describes one error Run joined: a *PanicError recovered from the Runner named panicked,
// or an error wrapping is.
type wantErr struct {
	panicked string
	is       error
}

// checkJoined checks that err joins exactly the errors want describes, in order.
func checkJoined(t *testing.T, err error, want []wantErr) {
	t.Helper()
	errs := joined(t, err)
	if len(errs) != len(want) {
		t.Fatalf("Run joined %d errors (%v), want %d", len(errs), errs, len(want))
	}
	for i, w := range want {
		if w.is != nil && !errors.Is(errs[i], w.is) {
			t.Errorf("error %d = %v, want it to wrap %v", i, errs[i], w.is)
		}
		if w.panicked == "" {
			continue
		}
		var pe *PanicError
		if !errors.As(errs[i], &pe) {
			t.Errorf("error %d = %v, want a *PanicError", i, errs[i])
			continue
		}
		if pe.Runner != w.panicked {
			t.Errorf("error %d is a panic of %q, want %q", i, pe.Runner, w.panicked)
		}
	}
}

// checkPanic checks that err holds a *PanicError recovered from runner with value, whose stack is
// that of the goroutine running method. A nil value stands for panic(nil), which recover reports
// as a *runtime.PanicNilError.
func checkPanic(t *testing.T, err error, runner string, value any, method string) {
	t.Helper()
	var pe *PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("Run = %v, want a *PanicError", err)
	}
	if pe.Runner != runner {
		t.Errorf("PanicError.Runner = %q, want %q", pe.Runner, runner)
	}
	if value == nil {
		var nilPanic *runtime.PanicNilError
		if !errors.As(err, &nilPanic) {
			t.Errorf("PanicError.Value = %v, want a *runtime.PanicNilError", pe.Value)
		}
	} else if pe.Value != value {
		t.Errorf("PanicError.Value = %v, want %v", pe.Value, value)
	}
	if frame := "(*scriptedRunner)." + method; !strings.Contains(string(pe.Stack), frame) {
		t.Errorf("PanicError.Stack does not contain %s:\n%s", frame, pe.Stack)
	}
	if cause, ok := value.(error); ok && !errors.Is(err, cause) {
		t.Errorf("Run = %v, want it to wrap the panic value %v", err, cause)
	}
}

func TestRunPanicExitUnderPanicFailBeginsShutdown(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "error value", value: errors.New("cause")},
		{name: "string value", value: "oops"},
		{name: "nil value", value: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
			h := runScriptedWith(t, []Option{WithPanicPolicy(PanicFail)}, a, b, c)
			b.panicExit(tt.value)

			err := <-h.result
			checkJoined(t, err, []wantErr{{panicked: "b"}})
			checkPanic(t, err, "b", tt.value, "panicExit")
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			h.finish()
		})
	}
}

func TestRunIsolatesAPanicExitAndKeepsTheSiblingsRunning(t *testing.T) {
	boom, stopBoom := errors.New("boom"), errors.New("stop boom")
	tests := []struct {
		name string
		// setup configures runners a, b and c before Run starts.
		setup func(a, b, c *scriptedRunner)
		// panics lists, by index, the Runners that exit with a panic, in the order they do.
		panics []int
		// end begins shutdown once every panic has been isolated.
		end  func(h *harness)
		want []wantErr
	}{
		{
			name:   "only problem is the panic",
			panics: []int{1},
			end:    func(h *harness) { h.cancelRun() },
			want:   []wantErr{{panicked: "b"}},
		},
		{
			name:   "panics joined in start order",
			panics: []int{2, 0},
			end:    func(h *harness) { h.cancelRun() },
			want:   []wantErr{{panicked: "a"}, {panicked: "c"}},
		},
		{
			name:   "a later failure comes first",
			panics: []int{1},
			end:    func(h *harness) { h.runners[2].exit(boom) },
			want:   []wantErr{{is: boom}, {panicked: "b"}},
		},
		{
			name:   "stop problems come after the panic",
			setup:  func(a, _, _ *scriptedRunner) { a.stopErr = stopBoom },
			panics: []int{1},
			end:    func(h *harness) { h.cancelRun() },
			want:   []wantErr{{panicked: "b"}, {is: stopBoom}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
			if tt.setup != nil {
				tt.setup(a, b, c)
			}
			h := runScriptedWith(t, []Option{WithPanicPolicy(PanicIsolate)}, a, b, c)
			for _, i := range tt.panics {
				h.runners[i].panicExit(errors.New(h.runners[i].name))
				<-h.waiting
			}

			for _, r := range h.runners {
				if len(r.stopCalled) != 0 {
					t.Errorf("%s stopped while a sibling's panic was isolated", r.name)
				}
			}

			tt.end(h)
			err := <-h.result
			checkJoined(t, err, tt.want)
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			h.finish()
		})
	}
}

func TestRunIsolatedPanicIsTheResultWhenItIsTheOnlyProblem(t *testing.T) {
	cause := errors.New("cause")
	a, b := newScripted("a"), newScripted("b")
	h := runScriptedWith(t, []Option{WithPanicPolicy(PanicIsolate)}, a, b)
	a.panicExit(cause)
	<-h.waiting
	h.cancelRun()

	err := <-h.result
	if err == nil {
		t.Fatalf("Run = nil, want the isolated panic")
	}
	checkPanic(t, err, "a", cause, "panicExit")
	h.finish()
}

func TestRunReportsAPanicDuringShutdownAsAStopProblem(t *testing.T) {
	cause, stopBoom := errors.New("cause"), errors.New("stop boom")
	tests := []struct {
		name   string
		policy PanicPolicy
		// setup configures runners a and b, stopped in that order after c.
		setup  func(a, b *scriptedRunner)
		want   []wantErr
		method string
	}{
		{
			name:   "stop panic under fail",
			policy: PanicFail,
			setup:  func(a, b *scriptedRunner) { b.stopPanic = cause; a.stopErr = stopBoom },
			want:   []wantErr{{panicked: "b", is: cause}, {is: stopBoom}},
			method: "Stop",
		},
		{
			name:   "stop panic under isolate",
			policy: PanicIsolate,
			setup:  func(a, b *scriptedRunner) { b.stopPanic = cause; a.stopErr = stopBoom },
			want:   []wantErr{{panicked: "b", is: cause}, {is: stopBoom}},
			method: "Stop",
		},
		{
			name:   "panic exit on stop under fail",
			policy: PanicFail,
			setup:  func(a, b *scriptedRunner) { b.stopExitPanic = cause; a.stopErr = stopBoom },
			want:   []wantErr{{panicked: "b", is: cause}, {is: stopBoom}},
			method: "panicExit",
		},
		{
			name:   "panic exit on stop under isolate",
			policy: PanicIsolate,
			setup:  func(a, b *scriptedRunner) { b.stopExitPanic = cause; a.stopErr = stopBoom },
			want:   []wantErr{{panicked: "b", is: cause}, {is: stopBoom}},
			method: "panicExit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
			tt.setup(a, b)
			h := runScriptedWith(t, []Option{WithPanicPolicy(tt.policy)}, a, b, c)
			h.cancelRun()

			err := <-h.result
			checkJoined(t, err, tt.want)
			checkPanic(t, joined(t, err)[0], "b", cause, tt.method)
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			h.finish()
		})
	}
}

func TestRunCallsStopOnceOnARunnerThatExitedWithAPanic(t *testing.T) {
	tests := []struct {
		name   string
		policy PanicPolicy
	}{
		{name: "fail", policy: PanicFail},
		{name: "isolate", policy: PanicIsolate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := newScripted("a"), newScripted("b")
			h := runScriptedWith(t, []Option{WithPanicPolicy(tt.policy)}, a, b)
			a.panicExit("oops")
			if tt.policy == PanicIsolate {
				<-h.waiting
				h.cancelRun()
			}

			checkPanic(t, <-h.result, "a", "oops", "panicExit")
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"b", "a"})
			h.finish()
		})
	}
}

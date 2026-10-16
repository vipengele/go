package daemon

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct{ name string }

func (f *fakeRunner) Name() string                { return f.name }
func (f *fakeRunner) Start(context.Context) error { return nil }
func (f *fakeRunner) Stop(context.Context) error  { return nil }

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

// recorder is the ordered log of Stop calls shared by the scripted Runners of one test.
type recorder struct {
	mu    sync.Mutex
	stops []string
}

func (r *recorder) stopped(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops = append(r.stops, name)
}

func (r *recorder) stopOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.stops)
}

// scriptedRunner is a Runner driven by its test through channels. Start reports its context on
// started and returns what the test sends on exit, panics with what the test sends on startPanic,
// or returns the cancellation of its context. Stop reports itself on stopCalled and returns
// stopErr, or panics with stopPanic. With hangStart or hangStop the call blocks until the test
// closes release.
type scriptedRunner struct {
	name string
	rec  *recorder

	started    chan context.Context
	exit       chan error
	startPanic chan any
	stopCalled chan struct{}
	release    chan struct{}

	hangStart bool
	hangStop  bool
	stopErr   error

	// stopPanic, when set, is what Stop panics with after reporting itself.
	stopPanic any

	// cancelPanic, when set, is what Start panics with once its context is cancelled.
	cancelPanic any

	// cancelErr, when set, is what Start returns once its context is cancelled.
	cancelErr error

	// onStop, when set, runs inside Stop before it returns.
	onStop func()
}

func newScripted(name string) *scriptedRunner {
	return &scriptedRunner{
		name:       name,
		started:    make(chan context.Context, 1),
		exit:       make(chan error, 1),
		startPanic: make(chan any, 1),
		stopCalled: make(chan struct{}, 1),
		release:    make(chan struct{}),
	}
}

func (r *scriptedRunner) Name() string { return r.name }

func (r *scriptedRunner) Start(ctx context.Context) error {
	r.started <- ctx
	if r.hangStart {
		select {
		case err := <-r.exit:
			return err
		case <-r.release:
			return nil
		}
	}
	select {
	case err := <-r.exit:
		return err
	case v := <-r.startPanic:
		panic(v)
	case <-ctx.Done():
		if r.cancelPanic != nil {
			panic(r.cancelPanic)
		}
		if r.cancelErr != nil {
			return r.cancelErr
		}
		return fmt.Errorf("%s: %w", r.name, ctx.Err())
	}
}

func (r *scriptedRunner) Stop(context.Context) error {
	r.rec.stopped(r.name)
	r.stopCalled <- struct{}{}
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

// waitingContext is a run context that reports each call to Done on waiting. Run's wait for
// shutdown calls Done once each time it starts waiting, so the second report means Run has dealt
// with a Start that returned or panicked and is waiting again.
type waitingContext struct {
	context.Context
	waiting chan struct{}
}

func (c *waitingContext) Done() <-chan struct{} {
	select {
	case c.waiting <- struct{}{}:
	default:
	}
	return c.Context.Done()
}

// ctxKey keys the value the run context carries, to check that each Start context inherits it.
type ctxKey struct{}

// harness runs a Supervisor over scripted Runners. Its stop context has no deadline: the test
// ends the grace period by calling cancelStop.
type harness struct {
	t          *testing.T
	baseline   int
	rec        *recorder
	runners    []*scriptedRunner
	startCtx   []context.Context
	cancelRun  context.CancelFunc
	cancelStop context.CancelFunc
	result     chan error

	// waiting receives a value each time Run starts waiting for shutdown.
	waiting chan struct{}
}

// runScripted starts Run over runners and returns once every Start has been entered.
func runScripted(t *testing.T, runners ...*scriptedRunner) *harness {
	t.Helper()
	return runScriptedWith(t, nil, runners...)
}

// runScriptedWith is runScripted with options for New.
func runScriptedWith(t *testing.T, opts []Option, runners ...*scriptedRunner) *harness {
	t.Helper()
	h := &harness{t: t, baseline: runtime.NumGoroutine(), rec: &recorder{}, runners: runners}
	list := make([]Runner, len(runners))
	for i, r := range runners {
		r.rec = h.rec
		list[i] = r
	}
	s, err := New(list, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stopCtx, cancelStop := context.WithCancel(context.Background())
	s.newStopContext = func() (context.Context, context.CancelFunc) { return stopCtx, cancelStop }
	runCtx, cancelRun := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "run"))
	h.cancelRun, h.cancelStop = cancelRun, cancelStop
	h.waiting = make(chan struct{}, 16)
	waitCtx := &waitingContext{Context: runCtx, waiting: h.waiting}

	h.result = make(chan error, 1)
	go func() { h.result <- s.Run(waitCtx) }()
	for _, r := range runners {
		h.startCtx = append(h.startCtx, <-r.started)
	}
	return h
}

// finish releases every hung call and checks that no goroutine of the run outlives it.
func (h *harness) finish() {
	h.t.Helper()
	for _, r := range h.runners {
		close(r.release)
	}
	h.cancelRun()
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

func TestRunStopsInReverseOrderAndReturnsNilOnParentCancel(t *testing.T) {
	h := runScripted(t, newScripted("a"), newScripted("b"), newScripted("c"))
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if got, want := h.rec.stopOrder(), []string{"c", "b", "a"}; !slices.Equal(got, want) {
		t.Errorf("stop order = %v, want %v", got, want)
	}
	h.finish()
}

func TestRunFailureStopsEveryRunnerAndIsReturnedFirst(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		failing int
		exitErr error
		want    error
	}{
		{name: "start error from the first runner", failing: 0, exitErr: boom, want: boom},
		{name: "start error from the last runner", failing: 2, exitErr: boom, want: boom},
		{name: "early nil from the middle runner", failing: 1, want: ErrRunnerExited},
		{name: "early nil from the first runner", failing: 0, want: ErrRunnerExited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := runScripted(t, newScripted("a"), newScripted("b"), newScripted("c"))
			failing := h.runners[tt.failing]
			failing.exit <- tt.exitErr

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
			if got, want := h.rec.stopOrder(), []string{"c", "b", "a"}; !slices.Equal(got, want) {
				t.Errorf("stop order = %v, want %v", got, want)
			}
			h.finish()
		})
	}
}

func TestRunDropsStartErrorsThatReportCancellation(t *testing.T) {
	a, b := newScripted("a"), newScripted("b")
	b.cancelErr = fmt.Errorf("serve: %w", context.Canceled)
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

func TestRunReportsAStopProblemAndStillStopsTheRest(t *testing.T) {
	stopBoom := errors.New("stop boom")
	tests := []struct {
		name string
		// setup configures runner b, which is stopped second of three.
		setup func(b *scriptedRunner)
		// hangs reports that b blocks until the grace period ends.
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
			name:  "start hangs past the deadline",
			setup: func(b *scriptedRunner) { b.hangStart = true },
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
			if got, want := h.rec.stopOrder(), []string{"c", "b", "a"}; !slices.Equal(got, want) {
				t.Errorf("stop order = %v, want %v", got, want)
			}
			if err := h.startCtx[0].Err(); !errors.Is(err, context.Canceled) {
				t.Errorf("a's Start context = %v, want it cancelled", err)
			}
			h.finish()
		})
	}
}

func TestRunKeepsEachStartContextLiveUntilItsTurn(t *testing.T) {
	a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
	var liveWhileCStops, liveWhileBStops []bool
	h := runScripted(t, a, b, c)
	c.onStop = func() { liveWhileCStops = []bool{h.startCtx[0].Err() == nil, h.startCtx[1].Err() == nil} }
	b.onStop = func() { liveWhileBStops = []bool{h.startCtx[0].Err() == nil} }

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if !slices.Equal(liveWhileCStops, []bool{true, true}) {
		t.Errorf("a and b Start contexts live while c stops = %v, want both live", liveWhileCStops)
	}
	if !slices.Equal(liveWhileBStops, []bool{true}) {
		t.Errorf("a Start context live while b stops = %v, want live", liveWhileBStops)
	}
	for i, ctx := range h.startCtx {
		if got := ctx.Value(ctxKey{}); got != "run" {
			t.Errorf("Start context %d carries %v, want the run context's value", i, got)
		}
	}
	h.finish()
}

func TestRunJoinsTheFailureFirstThenStopProblemsInStopOrder(t *testing.T) {
	startBoom, stopBoom, flushBoom := errors.New("start boom"), errors.New("stop boom"), errors.New("flush boom")
	a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
	b.cancelErr = flushBoom
	c.stopErr = stopBoom
	h := runScripted(t, a, b, c)
	a.exit <- startBoom

	errs := joined(t, <-h.result)
	want := []error{startBoom, stopBoom, flushBoom}
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

func TestRunStartPanicUnderPanicFailBeginsShutdown(t *testing.T) {
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
			b.startPanic <- tt.value

			err := <-h.result
			checkJoined(t, err, []wantErr{{panicked: "b"}})
			checkPanic(t, err, "b", tt.value, "Start")
			if got, want := h.rec.stopOrder(), []string{"c", "b", "a"}; !slices.Equal(got, want) {
				t.Errorf("stop order = %v, want %v", got, want)
			}
			h.finish()
		})
	}
}

func TestRunIsolatesAStartPanicAndKeepsTheSiblingsRunning(t *testing.T) {
	boom, stopBoom := errors.New("boom"), errors.New("stop boom")
	tests := []struct {
		name string
		// setup configures runners a, b and c before Run starts.
		setup func(a, b, c *scriptedRunner)
		// panics lists, by index, the Runners whose Start panics, in the order they panic.
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
			end:    func(h *harness) { h.runners[2].exit <- boom },
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
			<-h.waiting
			panicked := make(map[int]bool, len(tt.panics))
			for _, i := range tt.panics {
				h.runners[i].startPanic <- errors.New(h.runners[i].name)
				<-h.waiting
				panicked[i] = true
			}

			for i, r := range h.runners {
				if len(r.stopCalled) != 0 {
					t.Errorf("%s stopped while a sibling's panic was isolated", r.name)
				}
				if !panicked[i] && h.startCtx[i].Err() != nil {
					t.Errorf("%s Start context = %v, want it live while a sibling's panic is isolated",
						r.name, h.startCtx[i].Err())
				}
			}

			tt.end(h)
			err := <-h.result
			checkJoined(t, err, tt.want)
			if got, want := h.rec.stopOrder(), []string{"c", "b", "a"}; !slices.Equal(got, want) {
				t.Errorf("stop order = %v, want %v", got, want)
			}
			h.finish()
		})
	}
}

func TestRunIsolatedPanicIsTheResultWhenItIsTheOnlyProblem(t *testing.T) {
	cause := errors.New("cause")
	a, b := newScripted("a"), newScripted("b")
	h := runScriptedWith(t, []Option{WithPanicPolicy(PanicIsolate)}, a, b)
	<-h.waiting
	a.startPanic <- cause
	<-h.waiting
	h.cancelRun()

	err := <-h.result
	if err == nil {
		t.Fatalf("Run = nil, want the isolated panic")
	}
	checkPanic(t, err, "a", cause, "Start")
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
			name:   "start panic on cancellation under fail",
			policy: PanicFail,
			setup:  func(a, b *scriptedRunner) { b.cancelPanic = cause; a.stopErr = stopBoom },
			want:   []wantErr{{panicked: "b", is: cause}, {is: stopBoom}},
			method: "Start",
		},
		{
			name:   "start panic on cancellation under isolate",
			policy: PanicIsolate,
			setup:  func(a, b *scriptedRunner) { b.cancelPanic = cause; a.stopErr = stopBoom },
			want:   []wantErr{{panicked: "b", is: cause}, {is: stopBoom}},
			method: "Start",
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
			if got, want := h.rec.stopOrder(), []string{"c", "b", "a"}; !slices.Equal(got, want) {
				t.Errorf("stop order = %v, want %v", got, want)
			}
			h.finish()
		})
	}
}

func TestRunCallsStopOnceOnARunnerWhoseStartPanicked(t *testing.T) {
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
			<-h.waiting
			a.startPanic <- "oops"
			if tt.policy == PanicIsolate {
				<-h.waiting
				h.cancelRun()
			}

			checkPanic(t, <-h.result, "a", "oops", "Start")
			if got, want := h.rec.stopOrder(), []string{"b", "a"}; !slices.Equal(got, want) {
				t.Errorf("stop order = %v, want %v", got, want)
			}
			h.finish()
		})
	}
}

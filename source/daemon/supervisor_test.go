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
// started and returns what the test sends on exit, or the cancellation of its context. Stop
// reports itself on stopCalled and returns stopErr. With hangStart or hangStop the call blocks
// until the test closes release.
type scriptedRunner struct {
	name string
	rec  *recorder

	started    chan context.Context
	exit       chan error
	stopCalled chan struct{}
	release    chan struct{}

	hangStart bool
	hangStop  bool
	stopErr   error

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
	case <-ctx.Done():
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
	if r.hangStop {
		<-r.release
	}
	return r.stopErr
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
}

// runScripted starts Run over runners and returns once every Start has been entered.
func runScripted(t *testing.T, runners ...*scriptedRunner) *harness {
	t.Helper()
	h := &harness{t: t, baseline: runtime.NumGoroutine(), rec: &recorder{}, runners: runners}
	list := make([]Runner, len(runners))
	for i, r := range runners {
		r.rec = h.rec
		list[i] = r
	}
	s, err := New(list)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stopCtx, cancelStop := context.WithCancel(context.Background())
	s.newStopContext = func() (context.Context, context.CancelFunc) { return stopCtx, cancelStop }
	runCtx, cancelRun := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "run"))
	h.cancelRun, h.cancelStop = cancelRun, cancelStop

	h.result = make(chan error, 1)
	go func() { h.result <- s.Run(runCtx) }()
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

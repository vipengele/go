package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"
)

// manualClock is a Clock that moves only when the test advances it. Advance runs each timer that
// falls due in the goroutine that calls it, so the contexts a timer ends have ended once Advance
// returns.
type manualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
}

// manualTimer is one timer armed on a manualClock.
type manualTimer struct {
	when time.Time
	f    func()
}

func newManualClock() *manualClock {
	return &manualClock{now: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	t := &manualTimer{when: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		i := slices.Index(c.timers, t)
		if i < 0 {
			return false
		}
		c.timers = slices.Delete(c.timers, i, i+1)
		return true
	}
}

// Advance moves the clock on by d and runs every timer that has fallen due, in order of their time.
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*manualTimer
	c.timers = slices.DeleteFunc(c.timers, func(t *manualTimer) bool {
		if t.when.After(c.now) {
			return false
		}
		due = append(due, t)
		return true
	})
	c.mu.Unlock()
	slices.SortStableFunc(due, func(a, b *manualTimer) int { return a.when.Compare(b.when) })
	for _, t := range due {
		t.f()
	}
}

// pending returns the number of timers armed and neither fired nor stopped.
func (c *manualClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// awaitNoTimers yields until clock has no pending timer, and fails the test when a bounded number
// of yields does not get it there.
func awaitNoTimers(t *testing.T, clock *manualClock) {
	t.Helper()
	for range 1_000_000 {
		if clock.pending() == 0 {
			return
		}
		runtime.Gosched()
	}
	t.Errorf("%d timers pending, want none", clock.pending())
}

// checkDone fails the test unless ctx has ended with want.
func checkDone(t *testing.T, ctx context.Context, want error) {
	t.Helper()
	select {
	case <-ctx.Done():
	default:
		t.Fatalf("context not done, want it ended with %v", want)
	}
	if err := ctx.Err(); !errors.Is(err, want) {
		t.Errorf("Err() = %v, want %v", err, want)
	}
}

// checkLive fails the test when ctx has ended.
func checkLive(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatalf("context done with %v, want it live", ctx.Err())
	default:
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestWithDeadlineOnTheSystemClockIsAContextWithTimeout(t *testing.T) {
	ctx, cancel := withDeadline(realClock{}, context.Background(), time.Minute)
	defer cancel()
	if _, custom := ctx.(*clockContext); custom {
		t.Fatal("withDeadline on the system clock returned its own context")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Minute {
		t.Errorf("Deadline() = %v, %t, want at most a minute away", deadline, ok)
	}
}

func TestWithDeadlineReadsItsDeadlineFromTheClock(t *testing.T) {
	clock := newManualClock()
	ctx, cancel := withDeadline(clock, context.Background(), time.Minute)
	defer cancel()

	checkDeadline(t, ctx, clock.Now().Add(time.Minute))
	checkLive(t, ctx)
}

func TestWithDeadlineTakesTheParentsEarlierDeadline(t *testing.T) {
	clock := newManualClock()
	parent, cancelParent := withDeadline(clock, context.Background(), time.Second)
	defer cancelParent()
	ctx, cancel := withDeadline(clock, parent, time.Hour)
	defer cancel()

	want, _ := parent.Deadline()
	checkDeadline(t, ctx, want)
	clock.Advance(time.Second)
	<-ctx.Done()
	if err := ctx.Err(); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Err() = %v, want context.DeadlineExceeded from the parent", err)
	}
}

func TestWithDeadlineKeepsItsOwnEarlierDeadline(t *testing.T) {
	clock := newManualClock()
	parent, cancelParent := withDeadline(clock, context.Background(), time.Hour)
	defer cancelParent()
	ctx, cancel := withDeadline(clock, parent, time.Second)
	defer cancel()

	checkDeadline(t, ctx, clock.Now().Add(time.Second))
}

func TestWithDeadlineExpiresWhenTheClockReachesTheDeadline(t *testing.T) {
	clock := newManualClock()
	ctx, cancel := withDeadline(clock, context.Background(), time.Minute)
	defer cancel()

	clock.Advance(time.Minute - time.Nanosecond)
	checkLive(t, ctx)
	clock.Advance(time.Nanosecond)
	checkDone(t, ctx, context.DeadlineExceeded)
	if n := clock.pending(); n != 0 {
		t.Errorf("%d timers pending after the deadline, want none", n)
	}
}

func TestWithDeadlineEndsWithTheParent(t *testing.T) {
	clock := newManualClock()
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := withDeadline(clock, parent, time.Minute)
	defer cancel()

	cancelParent()
	<-ctx.Done()
	checkDone(t, ctx, context.Canceled)
	// The timer is stopped once the parent has ended it, and its firing changes nothing.
	awaitNoTimers(t, clock)
	clock.Advance(time.Minute)
	checkDone(t, ctx, context.Canceled)
}

func TestWithDeadlineOfAnEndedParentHasEnded(t *testing.T) {
	clock := newManualClock()
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	ctx, cancel := withDeadline(clock, parent, time.Minute)
	defer cancel()

	checkDone(t, ctx, context.Canceled)
	if n := clock.pending(); n != 0 {
		t.Errorf("%d timers armed for an ended parent, want none", n)
	}
}

func TestWithDeadlineOfNoTimeHasExpired(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		clock := newManualClock()
		ctx, cancel := withDeadline(clock, context.Background(), d)
		checkDone(t, ctx, context.DeadlineExceeded)
		checkDeadline(t, ctx, clock.Now().Add(d))
		if n := clock.pending(); n != 0 {
			t.Errorf("d=%v: %d timers armed, want none", d, n)
		}
		cancel()
		checkDone(t, ctx, context.DeadlineExceeded)
	}
}

func TestWithDeadlineFirstEndWins(t *testing.T) {
	clock := newManualClock()
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := withDeadline(clock, parent, time.Minute)

	clock.Advance(time.Minute)
	checkDone(t, ctx, context.DeadlineExceeded)
	cancelParent()
	cancel()
	checkDone(t, ctx, context.DeadlineExceeded)
}

func TestWithDeadlineCancelStopsTheTimer(t *testing.T) {
	baseline := runtime.NumGoroutine()
	clock := newManualClock()
	var stops []bool
	stopping := &stopRecordingClock{manualClock: clock, stops: &stops}
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	ctx, cancel := withDeadline(stopping, parent, time.Minute)

	cancel()
	checkDone(t, ctx, context.Canceled)
	if !slices.Equal(stops, []bool{true}) {
		t.Errorf("timer stops reported %v, want [true]", stops)
	}
	if n := clock.pending(); n != 0 {
		t.Errorf("%d timers pending after cancel, want none", n)
	}
	clock.Advance(time.Minute)
	cancelParent()
	checkDone(t, ctx, context.Canceled)
	settle(t, baseline)
}

// stopRecordingClock is a manualClock that records what each stop of a timer reports.
type stopRecordingClock struct {
	*manualClock
	stops *[]bool
}

func (c *stopRecordingClock) AfterFunc(d time.Duration, f func()) func() bool {
	stop := c.manualClock.AfterFunc(d, f)
	return func() bool {
		stopped := stop()
		*c.stops = append(*c.stops, stopped)
		return stopped
	}
}

func TestWithDeadlineDelegatesValuesToTheParent(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxKey{}, "run")
	ctx, cancel := withDeadline(newManualClock(), parent, time.Minute)
	defer cancel()
	if got := ctx.Value(ctxKey{}); got != "run" {
		t.Errorf("Value() = %v, want the parent's value", got)
	}
}

func TestWithDeadlineIsSafeForEveryOrderOfTimerParentAndCancel(t *testing.T) {
	baseline := runtime.NumGoroutine()
	for i := range 300 {
		clock := newManualClock()
		parent, cancelParent := context.WithCancel(context.Background())
		ctx, cancel := withDeadline(clock, parent, time.Second)

		ends := []func(){
			cancel,
			cancelParent,
			func() { clock.Advance(time.Second) },
			func() { _, _ = ctx.Err(), ctx.Value(ctxKey{}) },
		}
		k := i % len(ends)
		ends = append(ends[k:], ends[:k]...)
		var wg sync.WaitGroup
		for _, end := range ends {
			wg.Go(end)
		}
		wg.Wait()

		<-ctx.Done()
		if err := ctx.Err(); !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("run %d: Err() = %v, want context.Canceled or context.DeadlineExceeded", i, err)
		}
		awaitNoTimers(t, clock)
	}
	settle(t, baseline)
}

func TestRealClockAfterFuncRunsF(t *testing.T) {
	fired := make(chan struct{})
	realClock{}.AfterFunc(time.Millisecond, func() { close(fired) })
	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("AfterFunc never ran f")
	}
}

func TestRealClockAfterFuncStopPreventsF(t *testing.T) {
	stop := realClock{}.AfterFunc(time.Hour, func() { t.Error("f ran after stop") })
	if !stop() {
		t.Error("stop() = false before the timer fired, want true")
	}
	if stop() {
		t.Error("second stop() = true, want false")
	}
}

// firingClock is a Clock whose AfterFunc runs f before it returns, so a deadline ends its context
// before withDeadline has recorded the stop functions. It counts the calls of each stop.
type firingClock struct {
	*manualClock
	stops int
}

func (c *firingClock) AfterFunc(_ time.Duration, f func()) func() bool {
	f()
	return func() bool {
		c.stops++
		return false
	}
}

// stopCountingContext is a live context whose AfterFunc, which context.AfterFunc calls, counts
// the calls of the stop it returns. Its Done is a channel of its own, so that context.AfterFunc
// takes it for a custom context rather than one of the standard library.
type stopCountingContext struct {
	context.Context
	done  chan struct{}
	stops int
}

func (c *stopCountingContext) Done() <-chan struct{} { return c.done }

func (c *stopCountingContext) AfterFunc(func()) func() bool {
	return func() bool {
		c.stops++
		return true
	}
}

func TestWithDeadlineEndedBeforeItsWatchersAreRecordedStopsThem(t *testing.T) {
	clock := &firingClock{manualClock: newManualClock()}
	parent := &stopCountingContext{Context: context.Background(), done: make(chan struct{})}
	ctx, cancel := withDeadline(clock, parent, time.Minute)
	defer cancel()

	checkDone(t, ctx, context.DeadlineExceeded)
	if parent.stops != 1 || clock.stops != 1 {
		t.Errorf("parent watcher stopped %d times and timer %d times, want each once", parent.stops, clock.stops)
	}
	cancel()
	checkDone(t, ctx, context.DeadlineExceeded)
	if parent.stops != 1 || clock.stops != 1 {
		t.Errorf("after cancel: parent watcher stopped %d times and timer %d times, want each once",
			parent.stops, clock.stops)
	}
}

func TestNewRejectsANilClock(t *testing.T) {
	if _, err := New([]Runner{&fakeRunner{"a"}}, WithClock(nil)); err == nil {
		t.Fatal("New with a nil clock succeeded")
	}
}

func TestNewDefaultsToTheSystemClock(t *testing.T) {
	s, err := New([]Runner{&fakeRunner{"a"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := s.clock.(realClock); !ok {
		t.Errorf("clock = %T, want the system clock", s.clock)
	}
}

// clockRun is a Run of a Supervisor on a fake clock, whose start, stop and reload contexts are the
// ones New builds. Its signal subscription is fake: the test sends a signal on the channel
// subscribed delivers.
type clockRun struct {
	cancel     context.CancelFunc
	result     chan error
	waiting    chan struct{}
	subscribed chan chan<- os.Signal
}

// runOnClock starts Run over list, whose Runners are scripted ones or types embedding one, with
// WithClock(clock) and opts, and returns at once.
func runOnClock(t *testing.T, clock Clock, opts []Option, list ...Runner) *clockRun {
	t.Helper()
	for _, r := range list {
		r.(interface{ script() *scriptedRunner }).script().rec = &recorder{}
	}
	opts = append([]Option{WithLogger(slog.New(slog.DiscardHandler)), WithClock(clock)}, opts...)
	s, err := New(list, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	run := &clockRun{result: make(chan error, 1), waiting: make(chan struct{}, 16), subscribed: make(chan chan<- os.Signal, 1)}
	s.waiting = func() {
		select {
		case run.waiting <- struct{}{}:
		default:
		}
	}
	s.notify = func(c chan<- os.Signal, _ ...os.Signal) { run.subscribed <- c }
	s.unnotify = func(chan<- os.Signal) {}

	ctx, cancel := context.WithCancel(context.Background())
	run.cancel = cancel
	go func() { run.result <- s.Run(ctx) }()
	return run
}

// checkDeadline fails the test unless ctx has the deadline want.
func checkDeadline(t *testing.T, ctx context.Context, want time.Time) {
	t.Helper()
	if got, ok := ctx.Deadline(); !ok || !got.Equal(want) {
		t.Errorf("Deadline() = %v, %t, want %v", got, ok, want)
	}
}

func TestStartTimeoutExpiresOnTheClock(t *testing.T) {
	baseline := runtime.NumGoroutine()
	clock := newManualClock()
	r := newScripted("a")
	r.hangStart = true
	run := runOnClock(t, clock, []Option{WithStartTimeout(time.Minute)}, r)
	defer run.cancel()

	startCtx := <-r.started
	checkDeadline(t, startCtx, clock.Now().Add(time.Minute))
	clock.Advance(time.Minute - time.Nanosecond)
	checkLive(t, startCtx)
	clock.Advance(time.Nanosecond)

	if err := <-run.result; !errors.Is(err, ErrStartTimeout) {
		t.Errorf("Run = %v, want ErrStartTimeout", err)
	}
	checkDone(t, startCtx, context.DeadlineExceeded)
	settle(t, baseline)
}

// stopCtxRunner is a scripted Runner that reports the context its Stop is given.
type stopCtxRunner struct {
	*scriptedRunner
	stopCtx chan context.Context
}

func (r stopCtxRunner) Stop(ctx context.Context) error {
	r.stopCtx <- ctx
	return r.scriptedRunner.Stop(ctx)
}

func TestGraceExpiresOnTheClock(t *testing.T) {
	baseline := runtime.NumGoroutine()
	clock := newManualClock()
	r := stopCtxRunner{scriptedRunner: newScripted("a"), stopCtx: make(chan context.Context, 1)}
	r.hangStop = true
	run := runOnClock(t, clock, []Option{WithGrace(time.Second)}, r)

	<-r.started
	<-run.waiting
	run.cancel()
	stopCtx := <-r.stopCtx
	checkDeadline(t, stopCtx, clock.Now().Add(time.Second))
	clock.Advance(time.Second)

	if err := <-run.result; !errors.Is(err, ErrStopTimeout) {
		t.Errorf("Run = %v, want ErrStopTimeout", err)
	}
	checkDone(t, stopCtx, context.DeadlineExceeded)
	r.free()
	settle(t, baseline)
}

func TestReloadRoundExpiresOnTheClock(t *testing.T) {
	baseline := runtime.NumGoroutine()
	clock := newManualClock()
	r := newReloading("a", &reloadLog[int]{})
	r.hang, r.honourCtx = true, true
	load := func(context.Context) (int, error) { return 1, nil }
	run := runOnClock(t, clock, []Option{WithStartTimeout(time.Minute), WithReload(load)}, r)
	defer run.cancel()

	hangup := <-run.subscribed
	<-r.started
	<-run.waiting
	// The start phase has ended, so the reload round's timer is the only one Advance can fire.
	awaitNoTimers(t, clock)
	hangup <- syscall.SIGHUP
	roundCtx := <-r.reloading
	checkDeadline(t, roundCtx, clock.Now().Add(time.Minute))
	clock.Advance(time.Minute)

	if err := <-run.result; !errors.Is(err, ErrReloadTimeout) {
		t.Errorf("Run = %v, want ErrReloadTimeout", err)
	}
	checkDone(t, roundCtx, context.DeadlineExceeded)
	settle(t, baseline)
}

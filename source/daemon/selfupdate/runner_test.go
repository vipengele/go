package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/daemon"
	"go.vipengele.org/daemon/daemontest"
)

const (
	testPath    = "/usr/local/bin/tool"
	testTimeout = 10 * time.Minute
	// failAfter bounds every wait of a test on the runner, so that a broken runner fails the test
	// rather than hanging it.
	failAfter = 10 * time.Second
)

// armingClock is a daemontest.Clock that reports the delay of every timer armed on it, so that a
// test knows the runner has armed its next wait before it advances the clock, and keeps every
// timer, so that a test can tell whether it was stopped and run its callback itself.
type armingClock struct {
	*daemontest.Clock
	armed  chan time.Duration
	timers *timerLog
}

// newArmingClock returns an armingClock that reads start until it is advanced.
func newArmingClock(start time.Time) armingClock {
	return armingClock{Clock: daemontest.NewClock(start), armed: make(chan time.Duration, 64), timers: &timerLog{}}
}

// AfterFunc arms the timer on the daemontest.Clock and keeps it before it reports its delay, so a
// test that has received the delay finds the timer with last.
func (c armingClock) AfterFunc(d time.Duration, f func()) func() bool {
	stop := c.Clock.AfterFunc(d, f)
	t := &armedTimer{d: d, f: f}
	c.timers.add(t)
	c.armed <- d
	return func() bool {
		t.stopped.Store(true)
		return stop()
	}
}

// armedTimer is one AfterFunc call on an armingClock: its delay, its callback, and whether its stop
// function has been called.
type armedTimer struct {
	d       time.Duration
	f       func()
	stopped atomic.Bool
}

// timerLog keeps the timers an armingClock armed, in the order it armed them.
type timerLog struct {
	mu     sync.Mutex
	timers []*armedTimer
}

func (l *timerLog) add(t *armedTimer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.timers = append(l.timers, t)
}

// last returns the latest timer armed with delay d.
func (l *timerLog) last(d time.Duration) *armedTimer {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.timers) - 1; i >= 0; i-- {
		if l.timers[i].d == d {
			return l.timers[i]
		}
	}
	return nil
}

// cycleCall is one call of the fake cycle: its context, the clock's time when it began, and the
// channel the test sends the call's return value on.
type cycleCall struct {
	ctx context.Context
	at  time.Time
	ret chan error
}

// record is one log record a captureHandler kept.
type record struct {
	level slog.Level
	msg   string
	attrs map[string]slog.Value
}

// captureHandler keeps every record logged through it.
type captureHandler struct {
	mu      *sync.Mutex
	records *[]record
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := record{level: r.Level, msg: r.Message, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, rec)
	return nil
}

func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h captureHandler) WithGroup(string) slog.Handler { return h }

// harness drives one runner over an armingClock and a fake cycle.
type harness struct {
	t     *testing.T
	clock armingClock
	start time.Time
	calls chan cycleCall
	quit  chan struct{}
	r     *runner

	logMu   sync.Mutex
	records []record
}

// newHarness builds a runner over a fake cycle and an armingClock. path is what the runner's path
// function returns, and pathErr makes it fail instead.
func newHarness(t *testing.T, settings Settings, pathErr error, opts ...RunnerOption) *harness {
	t.Helper()

	h := &harness{
		t:     t,
		start: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		calls: make(chan cycleCall),
		quit:  make(chan struct{}),
	}
	h.clock = newArmingClock(h.start)
	logger := slog.New(captureHandler{mu: &h.logMu, records: &h.records})

	cycle := func(ctx context.Context) error {
		c := cycleCall{ctx: ctx, at: h.clock.Now(), ret: make(chan error)}
		select {
		case h.calls <- c:
		case <-h.quit:
			return nil
		}
		select {
		case err := <-c.ret:
			return err
		case <-h.quit:
			return nil
		}
	}
	path := func() (string, error) {
		if pathErr != nil {
			return "", pathErr
		}
		return testPath, nil
	}

	opts = append([]RunnerOption{WithRunnerClock(h.clock), WithLogger(logger), WithCycleTimeout(testTimeout)}, opts...)
	r, err := newRunner(cycle, path, settings, opts...)
	if err != nil {
		t.Fatalf("newRunner() error = %v", err)
	}
	h.r = r

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), failAfter)
		defer cancel()
		if err := r.Stop(ctx); err != nil {
			t.Errorf("cleanup Stop() = %v", err)
		}
	})
	// Registered last, so it runs first: a cycle the test left blocked returns before Stop waits.
	t.Cleanup(func() { close(h.quit) })

	return h
}

// begin starts the runner and returns the delay it armed for the first check.
func (h *harness) begin() time.Duration {
	h.t.Helper()
	if err := h.r.Start(h.t.Context()); err != nil {
		h.t.Fatalf("Start() = %v", err)
	}
	return h.armed()
}

// armed returns the delay of the next timer the runner arms.
func (h *harness) armed() time.Duration {
	h.t.Helper()
	select {
	case d := <-h.clock.armed:
		return d
	case <-time.After(failAfter):
		h.t.Fatal("the runner armed no timer")
		return 0
	}
}

// wantArmed fails unless the next timer the runner arms has delay want.
func (h *harness) wantArmed(want time.Duration) {
	h.t.Helper()
	if got := h.armed(); got != want {
		h.t.Fatalf("armed a timer for %v, want %v", got, want)
	}
}

// wantNoArm fails if the runner has armed a timer not yet taken by armed.
func (h *harness) wantNoArm() {
	h.t.Helper()
	select {
	case d := <-h.clock.armed:
		h.t.Fatalf("armed a timer for %v, want none", d)
	default:
	}
}

// call returns the next call of the cycle, after the timer of its timeout was armed.
func (h *harness) call() cycleCall {
	h.t.Helper()
	h.wantArmed(testTimeout)
	select {
	case c := <-h.calls:
		return c
	case <-time.After(failAfter):
		h.t.Fatal("the cycle was not called")
		return cycleCall{}
	}
}

// wantNoCall fails if the cycle has been called.
func (h *harness) wantNoCall() {
	h.t.Helper()
	select {
	case c := <-h.calls:
		h.t.Fatalf("cycle called at %v, want no call", c.at)
	default:
	}
}

// check advances the clock by d, expects a cycle to begin exactly then, returns ret from it and
// expects the runner to arm next for the check after it.
func (h *harness) check(d time.Duration, ret error, next time.Duration) cycleCall {
	h.t.Helper()
	want := h.clock.Now().Add(d)
	h.clock.Advance(d)
	c := h.call()
	if !c.at.Equal(want) {
		h.t.Fatalf("cycle called at %v, want %v", c.at, want)
	}
	c.ret <- ret
	h.wantArmed(next)
	return c
}

// logged returns the records logged at level with msg.
func (h *harness) logged(level slog.Level, msg string) []record {
	h.logMu.Lock()
	defer h.logMu.Unlock()
	var out []record
	for _, r := range h.records {
		if r.level == level && r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

// waitDone fails unless the runner ends within failAfter.
func waitDone(t *testing.T, r *runner) {
	t.Helper()
	select {
	case <-r.Done():
	case <-time.After(failAfter):
		t.Fatal("the runner did not end")
	}
}

// stopAsync calls Stop in a goroutine and returns the channel its result arrives on.
func stopAsync(r *runner) <-chan error {
	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop(context.Background()) }()
	return stopped
}

// timer returns the latest timer the runner armed with delay d.
func (h *harness) timer(d time.Duration) *armedTimer {
	h.t.Helper()
	t := h.clock.timers.last(d)
	if t == nil {
		h.t.Fatalf("no timer armed for %v", d)
	}
	return t
}

// wantStopped fails unless the runner has called the stop function of t.
func (h *harness) wantStopped(t *armedTimer) {
	h.t.Helper()
	if !t.stopped.Load() {
		h.t.Errorf("the timer armed for %v was not stopped", t.d)
	}
}

// wantCallbackRunsNoCycle runs the callback of t, as a timer that fired before it was stopped
// does, and fails if it calls the cycle rather than returning.
func (h *harness) wantCallbackRunsNoCycle(t *armedTimer) {
	h.t.Helper()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		t.f()
	}()
	select {
	case <-returned:
	case c := <-h.calls:
		h.t.Fatalf("the callback of the timer armed for %v called the cycle at %v", t.d, c.at)
	case <-time.After(failAfter):
		h.t.Fatal("the callback did not return")
	}
}

func TestRunnerFirstCheckIsAtTheJitter(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := jitter(testPath, DefaultInterval)
	if j <= 0 {
		t.Fatalf("jitter(%q) = %v, want a positive jitter for this test", testPath, j)
	}

	if got := h.begin(); got != j {
		t.Fatalf("first check armed for %v, want the jitter %v", got, j)
	}
	h.clock.Advance(j - time.Nanosecond)
	h.wantNoCall()
	h.check(time.Nanosecond, nil, DefaultInterval)
}

func TestRunnerStartLogsTheFirstCheckAtDebug(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Settings{Interval: 2 * time.Hour, AutoApply: true}, nil)
	j := h.begin()

	got := h.logged(slog.LevelDebug, msgScheduled)
	if len(got) != 1 {
		t.Fatalf("logged %d %q records, want 1", len(got), msgScheduled)
	}
	if delay := got[0].attrs["delay"].Duration(); delay != j {
		t.Errorf("record names delay %v, want the jitter %v", delay, j)
	}
	if interval := got[0].attrs["interval"].Duration(); interval != 2*time.Hour {
		t.Errorf("record names interval %v, want 2h", interval)
	}
}

func TestRunnerChecksEveryInterval(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Settings{Interval: 2 * time.Hour, AutoApply: true}, nil)
	j := h.begin()
	h.check(j, nil, 2*time.Hour)
	for range 3 {
		h.clock.Advance(2*time.Hour - time.Nanosecond)
		h.wantNoCall()
		h.check(time.Nanosecond, nil, 2*time.Hour)
	}
}

func TestRunnerJitterUsesTheIntervalAtStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Settings{Interval: 5 * time.Hour}, nil)
	h.r.reload(Settings{Interval: 3 * time.Hour})

	if got, want := h.begin(), jitter(testPath, 3*time.Hour); got != want {
		t.Errorf("first check armed for %v, want %v", got, want)
	}
}

func TestRunnerUnresolvablePathChecksAtOnceAndWarns(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), errors.New("no executable"))
	if got := h.begin(); got != 0 {
		t.Fatalf("first check armed for %v, want 0", got)
	}
	if got := h.logged(slog.LevelWarn, msgPathUnresolved); len(got) != 1 {
		t.Errorf("logged %d %q warnings, want 1", len(got), msgPathUnresolved)
	}
	h.check(0, nil, DefaultInterval)
}

func TestRunnerReloadMovesAWaitingCheckToLastCheckPlusTheNewInterval(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	first := h.check(j, nil, DefaultInterval)

	h.clock.Advance(time.Hour)
	h.r.reload(Settings{Interval: 3 * time.Hour, AutoApply: true})
	h.wantArmed(2 * time.Hour)

	c := h.check(2*time.Hour, nil, 3*time.Hour)
	if want := first.at.Add(3 * time.Hour); !c.at.Equal(want) {
		t.Errorf("second check at %v, want %v", c.at, want)
	}
}

func TestRunnerReloadChecksAtOnceWhenTheNewIntervalHasPassed(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.check(j, nil, DefaultInterval)

	h.clock.Advance(5 * time.Hour)
	h.r.reload(Settings{Interval: 2 * time.Hour, AutoApply: true})
	h.wantArmed(0)
	h.check(0, nil, 2*time.Hour)
}

func TestRunnerReloadDoesNotMoveTheFirstCheck(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()

	h.r.reload(Settings{Interval: 2 * time.Hour, AutoApply: true})
	h.wantNoArm()
	h.check(j, nil, 2*time.Hour)
}

func TestRunnerReloadDuringACheckSchedulesTheNextWithTheNewInterval(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.clock.Advance(j)
	c := h.call()

	h.r.reload(Settings{Interval: 2 * time.Hour, AutoApply: true})
	h.wantNoArm()
	c.ret <- nil
	h.wantArmed(2 * time.Hour)
}

func TestRunnerReloadOfTheSameIntervalKeepsTheSchedule(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.check(j, nil, DefaultInterval)

	h.r.reload(Settings{AutoApply: true})
	h.wantNoArm()
}

func TestRunnerReloadTogglesAutoApply(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	if !h.r.autoApply() {
		t.Fatal("autoApply() = false, want true from DefaultSettings")
	}
	h.r.reload(Settings{Interval: DefaultInterval, AutoApply: false})
	if h.r.autoApply() {
		t.Error("autoApply() = true after a reload turning it off")
	}
	h.r.reload(Settings{Interval: DefaultInterval, AutoApply: true})
	if !h.r.autoApply() {
		t.Error("autoApply() = false after a reload turning it on")
	}
}

func TestRunnerReloadOfAnInvalidIntervalKeepsTheCurrentOneAndWarns(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Settings{Interval: 2 * time.Hour, AutoApply: true}, nil)
	j := h.begin()
	h.check(j, nil, 2*time.Hour)

	h.r.reload(Settings{Interval: time.Minute, AutoApply: false})
	h.wantNoArm()
	if h.r.autoApply() {
		t.Error("autoApply() = true, want the reload's false despite its invalid interval")
	}
	got := h.logged(slog.LevelWarn, msgReloadRejected)
	if len(got) != 1 {
		t.Fatalf("logged %d %q warnings, want 1", len(got), msgReloadRejected)
	}
	if kept := got[0].attrs["interval"].Duration(); kept != 2*time.Hour {
		t.Errorf("warning names interval %v, want the kept 2h", kept)
	}

	h.clock.Advance(2*time.Hour - time.Nanosecond)
	h.wantNoCall()
	h.check(time.Nanosecond, nil, 2*time.Hour)
}

func TestRunnerStopWhileWaitingForTheJitter(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	first := h.timer(j)

	if err := h.r.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	waitDone(t, h.r)
	if err := h.r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil after Stop", err)
	}
	h.wantStopped(first)
	h.clock.Advance(j)
	h.wantNoCall()
	h.wantNoArm()
}

func TestRunnerStopBetweenChecks(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.check(j, nil, DefaultInterval)
	next := h.timer(DefaultInterval)

	if err := h.r.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	waitDone(t, h.r)
	if err := h.r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil after Stop", err)
	}
	h.wantStopped(next)
	h.r.reload(Settings{Interval: time.Hour, AutoApply: true})
	h.clock.Advance(DefaultInterval)
	h.wantNoCall()
	h.wantNoArm()
}

func TestRunnerStopDuringACheckCancelsItAndWaitsForIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.clock.Advance(j)
	c := h.call()

	stopped := stopAsync(h.r)
	select {
	case <-c.ctx.Done():
	case <-time.After(failAfter):
		t.Fatal("Stop did not cancel the check's context")
	}
	select {
	case err := <-stopped:
		t.Fatalf("Stop() = %v before the check returned, want it to wait", err)
	case <-h.r.Done():
		t.Fatal("Done closed before the check returned")
	default:
	}

	c.ret <- c.ctx.Err()
	if err := <-stopped; err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	waitDone(t, h.r)
	if err := h.r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil after Stop", err)
	}
	if got := h.logged(slog.LevelWarn, msgCycleFailed); len(got) != 0 {
		t.Errorf("logged %d %q warnings for a check Stop cancelled, want 0", len(got), msgCycleFailed)
	}
	h.wantNoArm()
}

func TestRunnerStopReturnsTheContextErrorWhenACheckOutlastsIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.clock.Advance(j)
	c := h.call()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.r.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Stop() = %v, want context.Canceled", err)
	}
	c.ret <- nil
	waitDone(t, h.r)
}

func TestRunnerCheckThatOverrunsTheTimeoutIsCancelledAndTheRunnerKeepsTicking(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.clock.Advance(j)
	c := h.call()

	h.clock.Advance(testTimeout)
	select {
	case <-c.ctx.Done():
	case <-time.After(failAfter):
		t.Fatal("the timeout did not cancel the check's context")
	}
	if cause := context.Cause(c.ctx); !errors.Is(cause, context.DeadlineExceeded) {
		t.Errorf("context.Cause() = %v, want context.DeadlineExceeded", cause)
	}

	c.ret <- c.ctx.Err()
	h.wantArmed(DefaultInterval - testTimeout)
	if got := h.logged(slog.LevelWarn, msgCycleTimedOut); len(got) != 1 {
		t.Errorf("logged %d %q warnings, want 1", len(got), msgCycleTimedOut)
	}
	h.check(DefaultInterval-testTimeout, nil, DefaultInterval)
}

func TestRunnerCheckContextOutlivesTheStartContextAndKeepsItsValues(t *testing.T) {
	t.Parallel()

	type key struct{}
	h := newHarness(t, DefaultSettings(), nil)
	startCtx, cancel := context.WithCancel(context.WithValue(t.Context(), key{}, "v"))
	if err := h.r.Start(startCtx); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	cancel()
	j := h.armed()

	h.clock.Advance(j)
	c := h.call()
	if err := c.ctx.Err(); err != nil {
		t.Errorf("check context Err() = %v, want nil after the start context ended", err)
	}
	if got := c.ctx.Value(key{}); got != "v" {
		t.Errorf("check context Value() = %v, want the start context's v", got)
	}
	c.ret <- nil
	h.wantArmed(DefaultInterval)
}

func TestRunnerCheckRequiringARestartEndsTheRunner(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.clock.Advance(j)
	c := h.call()
	c.ret <- fmt.Errorf("installed v2: %w", daemon.ErrRestartRequired)

	waitDone(t, h.r)
	if err := h.r.Err(); !errors.Is(err, daemon.ErrRestartRequired) {
		t.Errorf("Err() = %v, want daemon.ErrRestartRequired", err)
	}
	h.r.mu.Lock()
	parent := h.r.ctx
	h.r.mu.Unlock()
	if parent.Err() == nil {
		t.Error("the parent context of the checks is not done once the runner has ended")
	}
	if got := h.logged(slog.LevelInfo, msgRestartRequired); len(got) != 1 {
		t.Errorf("logged %d %q records, want 1", len(got), msgRestartRequired)
	}
	h.wantNoArm()
	h.clock.Advance(2 * DefaultInterval)
	h.wantNoCall()

	if err := h.r.Stop(context.Background()); err != nil {
		t.Errorf("Stop() = %v", err)
	}
	if err := h.r.Err(); !errors.Is(err, daemon.ErrRestartRequired) {
		t.Errorf("Err() after Stop = %v, want daemon.ErrRestartRequired", err)
	}
}

func TestRunnerCheckRequiringARestartWinsOverAConcurrentStop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.clock.Advance(j)
	c := h.call()

	stopped := stopAsync(h.r)
	<-c.ctx.Done()
	c.ret <- fmt.Errorf("installed v2: %w", daemon.ErrRestartRequired)

	if err := <-stopped; err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	waitDone(t, h.r)
	if err := h.r.Err(); !errors.Is(err, daemon.ErrRestartRequired) {
		t.Errorf("Err() = %v, want daemon.ErrRestartRequired", err)
	}
	h.wantNoArm()
}

func TestRunnerKeepsTickingAfterAFailedCheck(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.check(j, errors.New("release server unreachable"), DefaultInterval)

	got := h.logged(slog.LevelWarn, msgCycleFailed)
	if len(got) != 1 {
		t.Fatalf("logged %d %q warnings, want 1", len(got), msgCycleFailed)
	}
	if err := got[0].attrs["err"].String(); !strings.Contains(err, "release server unreachable") {
		t.Errorf("warning err = %q, want the check's error", err)
	}
	select {
	case <-h.r.Done():
		t.Fatal("Done closed after a failed check")
	default:
	}
	h.check(DefaultInterval, nil, DefaultInterval)
}

func TestRunnerCheckThatPanicsEndsTheRunnerWithAPanicError(t *testing.T) {
	t.Parallel()

	clock := newArmingClock(time.Unix(0, 0))
	r, err := newRunner(
		func(context.Context) error { panic("boom") },
		func() (string, error) { return testPath, nil },
		DefaultSettings(),
		WithRunnerClock(clock), WithName("updater"),
	)
	if err != nil {
		t.Fatalf("newRunner() error = %v", err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	clock.Advance(<-clock.armed)

	waitDone(t, r)
	var panicErr *daemon.PanicError
	if err := r.Err(); !errors.As(err, &panicErr) || panicErr.Runner != "updater" || panicErr.Value != "boom" {
		t.Errorf("Err() = %v, want a *daemon.PanicError of runner updater with value boom", err)
	}
}

func TestRunnerStartTwiceFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	h.begin()
	if err := h.r.Start(t.Context()); !errors.Is(err, errRunnerStarted) {
		t.Errorf("second Start() = %v, want errRunnerStarted", err)
	}
	h.wantNoArm()
}

func TestRunnerStopBeforeStartLeavesItNeverStarted(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	if err := h.r.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if err := h.r.Start(t.Context()); !errors.Is(err, context.Canceled) {
		t.Errorf("Start() after Stop = %v, want context.Canceled", err)
	}
	h.wantNoArm()
	select {
	case <-h.r.Done():
		t.Error("Done closed for a runner never started")
	default:
	}
}

func TestNewRunnerDefaults(t *testing.T) {
	t.Parallel()

	r, err := newRunner(func(context.Context) error { return nil }, func() (string, error) { return "", nil }, Settings{})
	if err != nil {
		t.Fatalf("newRunner() error = %v", err)
	}
	if r.Name() != "selfupdate" {
		t.Errorf("Name() = %q, want selfupdate", r.Name())
	}
	if r.cycleTimeout != 10*time.Minute {
		t.Errorf("cycle timeout = %v, want 10m", r.cycleTimeout)
	}
	if r.interval != DefaultInterval {
		t.Errorf("interval = %v, want %v for a zero Interval", r.interval, DefaultInterval)
	}
	if _, ok := r.clock.(systemClock); !ok {
		t.Errorf("clock = %T, want the system clock", r.clock)
	}

	named, err := newRunner(func(context.Context) error { return nil }, func() (string, error) { return "", nil },
		Settings{}, WithName("updater"))
	if err != nil {
		t.Fatalf("newRunner(WithName) error = %v", err)
	}
	if named.Name() != "updater" {
		t.Errorf("Name() = %q, want updater", named.Name())
	}
}

func TestNewRunnerRejects(t *testing.T) {
	t.Parallel()

	cycle := func(context.Context) error { return nil }
	path := func() (string, error) { return "", nil }
	tests := []struct {
		name     string
		cycle    func(context.Context) error
		path     func() (string, error)
		settings Settings
		opts     []RunnerOption
	}{
		{name: "nil cycle", path: path},
		{name: "nil path", cycle: cycle},
		{name: "interval below the minimum", cycle: cycle, path: path, settings: Settings{Interval: 59 * time.Minute}},
		{name: "negative interval", cycle: cycle, path: path, settings: Settings{Interval: -time.Hour}},
		{name: "nil logger", cycle: cycle, path: path, opts: []RunnerOption{WithLogger(nil)}},
		{name: "nil clock", cycle: cycle, path: path, opts: []RunnerOption{WithRunnerClock(nil)}},
		{name: "blank name", cycle: cycle, path: path, opts: []RunnerOption{WithName(" ")}},
		{name: "zero cycle timeout", cycle: cycle, path: path, opts: []RunnerOption{WithCycleTimeout(0)}},
		{name: "negative cycle timeout", cycle: cycle, path: path, opts: []RunnerOption{WithCycleTimeout(-time.Second)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if r, err := newRunner(tt.cycle, tt.path, tt.settings, tt.opts...); err == nil {
				t.Errorf("newRunner() = %v, nil; want an error", r)
			}
		})
	}
}

func TestRunnerStopWhileStartResolvesThePathArmsNothing(t *testing.T) {
	t.Parallel()

	clock := newArmingClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	var r *runner
	stopErr := make(chan error, 1)
	path := func() (string, error) {
		stopErr <- r.Stop(context.Background())
		return testPath, nil
	}
	r, err := newRunner(func(context.Context) error {
		t.Error("cycle called on a runner stopped before it armed")
		return nil
	}, path, DefaultSettings(), WithRunnerClock(clock))
	if err != nil {
		t.Fatalf("newRunner() error = %v", err)
	}

	if err := r.Start(t.Context()); !errors.Is(err, context.Canceled) {
		t.Errorf("Start() = %v, want context.Canceled", err)
	}
	if err := <-stopErr; err != nil {
		t.Errorf("Stop() = %v, want nil", err)
	}
	waitDone(t, r)
	select {
	case d := <-clock.armed:
		t.Errorf("armed a timer for %v, want none", d)
	default:
	}
}

func TestRunnerReloadStopsTheTimerItReplacesAndItsFiredCallbackRunsNoCycle(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	h.check(j, nil, DefaultInterval)
	replaced := h.timer(DefaultInterval)

	h.clock.Advance(time.Hour)
	h.r.reload(Settings{Interval: 3 * time.Hour, AutoApply: true})
	h.wantArmed(2 * time.Hour)
	h.wantStopped(replaced)

	h.wantCallbackRunsNoCycle(replaced)
	h.wantNoArm()
	h.check(2*time.Hour, nil, 3*time.Hour)
}

func TestRunnerCheckContextEndsOnceTheCheckReturns(t *testing.T) {
	t.Parallel()

	h := newHarness(t, DefaultSettings(), nil)
	j := h.begin()
	c := h.check(j, nil, DefaultInterval)

	if err := c.ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("check context Err() = %v after the check returned, want context.Canceled", err)
	}
}

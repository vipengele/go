package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"go.vipengele.org/daemon"
)

// The messages a runner logs.
const (
	msgScheduled       = "self-update first check scheduled"
	msgPathUnresolved  = "self-update cannot resolve the binary path; the first check is not delayed"
	msgCycleTimedOut   = "self-update check timed out"
	msgCycleFailed     = "self-update check failed"
	msgRestartRequired = "self-update installed a new binary; restart required"
	msgReloadRejected  = "self-update interval rejected; keeping the current one"
)

// errRunnerStarted is what a second Start returns on a runner.
var errRunnerStarted = errors.New("selfupdate: runner already started")

var _ daemon.Runner = (*runner)(nil)

// runner is the daemon.Runner that schedules a daemon's update checks: it calls cycle once after a
// jitter derived from the binary's path, and then once every interval, one call at a time.
//
// Every wait is a timer on the clock, re-armed from the callback of the one before it; there is no
// goroutine of the runner's own between checks. A cycle runs on the goroutine of the timer that
// fired it, on a context derived from the Start context without its cancellation, which Stop
// cancels, and bounded by the cycle timeout.
//
// A cycle that returns an error matching daemon.ErrRestartRequired has installed a new binary: the
// runner ends with that error, even when Stop was called while it ran, and schedules nothing more.
// A cycle that panics ends the runner with a *daemon.PanicError. Any other error is logged and the
// next check is scheduled as usual.
type runner struct {
	cycle func(ctx context.Context) error
	path  func() (string, error)

	name         string
	logger       *slog.Logger
	clock        daemon.Clock
	cycleTimeout time.Duration

	// done closes once the runner has ended; err is written before it closes and read only after.
	done chan struct{}
	err  error

	// mu guards every field below.
	mu sync.Mutex
	// interval and autoApplyOn are the current Settings, as the constructor and reload set them.
	interval    time.Duration
	autoApplyOn bool
	// started is set by the first Start, stopped by Stop, ended once done is closed.
	started bool
	stopped bool
	ended   bool
	// checked is set once the first check has begun; lastCheck is when the latest one began.
	checked   bool
	lastCheck time.Time
	// running is set while a cycle runs.
	running bool
	// stopTimer stops the armed timer, and is nil while none is armed. gen numbers the armed
	// timer: a callback that finds another number was disarmed after it fired, and does nothing.
	stopTimer func() bool
	gen       uint64
	// ctx is the parent of every cycle's context and cancel cancels it; both are set by Start.
	ctx    context.Context
	cancel context.CancelFunc
}

// newRunner returns a runner that calls cycle on the schedule settings describe. path names the
// running binary, whose jitter delays the first check. settings is rejected when its Validate
// fails; a zero Interval means DefaultInterval.
func newRunner(
	cycle func(ctx context.Context) error,
	path func() (string, error),
	settings Settings,
	opts ...RunnerOption,
) (*runner, error) {
	if cycle == nil {
		return nil, errors.New("selfupdate: nil cycle")
	}
	if path == nil {
		return nil, errors.New("selfupdate: nil path function")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	settings = settings.withDefaults()

	r := &runner{
		cycle:        cycle,
		path:         path,
		name:         defaultRunnerName,
		logger:       slog.Default(),
		clock:        systemClock{},
		cycleTimeout: defaultCycleTimeout,
		done:         make(chan struct{}),
		interval:     settings.Interval,
		autoApplyOn:  settings.AutoApply,
	}
	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// Name returns the runner's name, "selfupdate" unless WithName set another.
func (r *runner) Name() string { return r.name }

// Start schedules the first check after the jitter of the binary's path and returns. A path that
// cannot be resolved is logged and the first check is not delayed. Start returns an error when the
// runner was already started, and one wrapping context.Canceled when Stop came first.
func (r *runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return fmt.Errorf("selfupdate: runner %q: %w", r.name, errRunnerStarted)
	}
	r.started = true
	if r.stopped {
		r.mu.Unlock()
		return r.stoppedBeforeStart()
	}
	r.ctx, r.cancel = context.WithCancel(context.WithoutCancel(ctx))
	interval := r.interval
	r.mu.Unlock()

	delay := r.firstDelay(ctx, interval)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return r.stoppedBeforeStart()
	}
	r.arm(delay)
	r.logger.LogAttrs(ctx, slog.LevelDebug, msgScheduled,
		slog.String("runner", r.name), slog.Duration("delay", delay), slog.Duration("interval", interval))

	return nil
}

// Done is closed once the runner has ended: after Stop, once an in-flight cycle has returned, or
// after a cycle that requires a restart or panicked.
func (r *runner) Done() <-chan struct{} { return r.done }

// Err returns, once Done is closed, the error of the cycle that ended the runner: one matching
// daemon.ErrRestartRequired, or a *daemon.PanicError. It is nil for a runner Stop ended, and nil
// before Done closes.
func (r *runner) Err() error {
	select {
	case <-r.done:
		return r.err
	default:
		return nil
	}
}

// Stop cancels the armed timer and the context of every cycle, and waits for an in-flight cycle to
// return. It returns ctx's error when ctx is done first, leaving the cycle to finish on its own.
// Stop before Start returns nil and leaves the runner never started.
func (r *runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.stopped = true
	if !r.started {
		r.mu.Unlock()
		return nil
	}
	r.disarm()
	if r.cancel != nil {
		r.cancel()
	}
	if !r.running {
		r.end(nil)
	}
	r.mu.Unlock()

	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// reload applies settings. AutoApply takes effect at once. A new valid Interval moves a check that
// is waiting to the last check plus the new Interval, or to now when that has passed; the first
// check, which waits for the jitter, does not move, and a check in flight schedules the next with
// the new Interval when it returns. An Interval that fails Validate is logged and the current one
// kept. reload never fails.
func (r *runner) reload(settings Settings) {
	invalid := settings.Validate()
	interval := settings.withDefaults().Interval

	r.mu.Lock()
	r.autoApplyOn = settings.AutoApply
	current := r.interval
	if invalid == nil && interval != r.interval {
		r.interval = interval
		if r.started && r.checked && !r.running && !r.stopped && !r.ended {
			r.disarm()
			r.arm(r.untilNext())
		}
	}
	r.mu.Unlock()

	if invalid != nil {
		r.logger.LogAttrs(context.Background(), slog.LevelWarn, msgReloadRejected,
			slog.String("runner", r.name), slog.Duration("interval", current), slog.Any("err", invalid))
	}
}

// autoApply reports whether a check that finds a newer release installs it.
func (r *runner) autoApply() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.autoApplyOn
}

// firstDelay returns the jitter of the binary's path for interval, or 0 when path fails.
func (r *runner) firstDelay(ctx context.Context, interval time.Duration) time.Duration {
	p, err := r.path()
	if err != nil {
		r.logger.LogAttrs(ctx, slog.LevelWarn, msgPathUnresolved, slog.String("runner", r.name), slog.Any("err", err))
		return 0
	}
	return jitter(p, interval)
}

// fire runs one cycle, unless the timer numbered gen was disarmed after it fired, and then
// schedules the next.
func (r *runner) fire(gen uint64) {
	r.mu.Lock()
	if gen != r.gen || r.running || r.stopped || r.ended {
		r.mu.Unlock()
		return
	}
	r.stopTimer = nil
	r.running = true
	r.checked = true
	r.lastCheck = r.clock.Now()
	parent := r.ctx
	r.mu.Unlock()

	timedOut, err := r.runCycle(parent)
	r.finishCycle(timedOut, err)
}

// runCycle calls cycle on a context derived from parent that ends once the cycle timeout has
// elapsed on the clock, with context.DeadlineExceeded as its cause. It reports whether that
// timeout ended the context, and what cycle returned, or a *daemon.PanicError when it panicked.
func (r *runner) runCycle(parent context.Context) (timedOut bool, err error) {
	ctx, cancel := context.WithCancelCause(parent)
	stopTimeout := r.clock.AfterFunc(r.cycleTimeout, func() { cancel(context.DeadlineExceeded) })
	defer func() {
		if v := recover(); v != nil {
			err = &daemon.PanicError{Runner: r.name, Value: v, Stack: debug.Stack()}
		}
		stopTimeout()
		timedOut = errors.Is(context.Cause(ctx), context.DeadlineExceeded)
		cancel(context.Canceled)
	}()

	return false, r.cycle(ctx)
}

// finishCycle ends the runner after a cycle that requires a restart or panicked, or that ran while
// Stop was called, and otherwise logs a failed cycle and schedules the next. It logs before it
// ends the runner or arms the timer, so the record precedes whatever the runner does next.
func (r *runner) finishCycle(timedOut bool, err error) {
	var panicErr *daemon.PanicError
	restart := errors.Is(err, daemon.ErrRestartRequired)
	panicked := errors.As(err, &panicErr)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false

	ctx := context.Background()
	runnerAttr := slog.String("runner", r.name)
	switch {
	case panicked:
		r.end(err)
	case restart:
		r.logger.LogAttrs(ctx, slog.LevelInfo, msgRestartRequired, runnerAttr, slog.Any("err", err))
		r.end(err)
	case r.stopped:
		r.end(nil)
	default:
		switch {
		case timedOut:
			r.logger.LogAttrs(ctx, slog.LevelWarn, msgCycleTimedOut,
				runnerAttr, slog.Duration("timeout", r.cycleTimeout), slog.Any("err", err))
		case err != nil:
			r.logger.LogAttrs(ctx, slog.LevelWarn, msgCycleFailed, runnerAttr, slog.Any("err", err))
		}
		r.arm(r.untilNext())
	}
}

// untilNext returns the time from now to the last check plus the interval, or 0 when that has
// passed. The caller holds mu.
func (r *runner) untilNext() time.Duration {
	return max(r.lastCheck.Add(r.interval).Sub(r.clock.Now()), 0)
}

// arm arms the timer that fires the next check after d. The caller holds mu and no timer is armed.
func (r *runner) arm(d time.Duration) {
	r.gen++
	gen := r.gen
	r.stopTimer = r.clock.AfterFunc(d, func() { r.fire(gen) })
}

// disarm stops the armed timer, if any; a callback of it that has already fired does nothing. The
// caller holds mu.
func (r *runner) disarm() {
	r.gen++
	if r.stopTimer != nil {
		r.stopTimer()
		r.stopTimer = nil
	}
}

// end ends the runner with err, unless it has already ended: it disarms the timer, cancels the
// context of every cycle and closes done. The caller holds mu.
func (r *runner) end(err error) {
	if r.ended {
		return
	}
	r.ended = true
	r.disarm()
	if r.cancel != nil {
		r.cancel()
	}
	r.err = err
	close(r.done)
}

// stoppedBeforeStart is what Start returns on a runner Stop reached first. It wraps
// context.Canceled, which the Supervisor takes as the end of the start phase, not a start failure.
func (r *runner) stoppedBeforeStart() error {
	return fmt.Errorf("selfupdate: runner %q: stopped before it started: %w", r.name, context.Canceled)
}

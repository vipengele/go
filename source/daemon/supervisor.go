package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
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

	// clock is what the start, stop and reload contexts measure their deadlines against.
	clock Clock

	// logger is the logger given to WithLogger, nil when none was; log is what Run resolved from
	// it.
	logger *slog.Logger
	log    *slog.Logger

	// instance, when set, is the instance lock Run holds while any Runner may be running.
	instance *instanceLock

	// health is the registry New registers the health checks in, or nil.
	health *Health

	// feed is the state of the run the health checks read.
	feed feed

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

	// reload, when set, is the configuration reload WithReload installed.
	reload *reloadHook

	// newReloadContext returns the context handed to one reload round and the function that
	// releases it. It carries the values and the cancellation of parent and bounds the round by the
	// start timeout.
	newReloadContext func(parent context.Context) (context.Context, context.CancelFunc)

	// notify and unnotify subscribe a channel to signals and cancel the subscription.
	notify   func(c chan<- os.Signal, sig ...os.Signal)
	unnotify func(c chan<- os.Signal)
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
		clock:        realClock{},
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

	clock, startTimeout, grace := s.clock, s.startTimeout, s.grace
	s.newStartContext = func(parent context.Context) (context.Context, context.CancelFunc) {
		return withDeadline(clock, parent, startTimeout)
	}
	s.newReloadContext = s.newStartContext
	s.notify, s.unnotify = signal.Notify, signal.Stop
	s.newStopContext = func() (context.Context, context.CancelFunc) {
		return withDeadline(clock, context.Background(), grace)
	}
	s.feed.track(s.runners)
	if s.health != nil {
		if err := s.health.addAll(s.checks()); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// checks returns the health checks of the Supervisor: supervisor.live, supervisor.ready, and one
// Readiness check per HealthChecker Runner, in start order.
func (s *Supervisor) checks() []namedCheck {
	checks := []namedCheck{
		{name: "supervisor.live", check: func(context.Context) error { return s.feed.live() }, kinds: []Kind{Liveness}},
		{name: "supervisor.ready", check: func(context.Context) error { return s.feed.ready() }, kinds: []Kind{Readiness}},
	}
	for _, r := range s.runners {
		hc, ok := r.(HealthChecker)
		if !ok {
			continue
		}
		name := r.Name()
		checks = append(checks, namedCheck{
			name: name,
			check: func(ctx context.Context) error {
				if err := s.feed.running(name); err != nil {
					return err
				}
				return hc.CheckHealth(ctx)
			},
			kinds: []Kind{Readiness},
		})
	}
	return checks
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
// With WithInstanceLock, Run takes the instance lock before the first Start and releases it as it
// returns, once every Runner has been stopped or, at the end of the grace period, abandoned. A
// lock that cannot be taken is returned as AcquireInstance reports it, an *AlreadyRunningError
// while another instance holds it, and no Runner is started.
//
// With WithReload, Run subscribes to SIGHUP once the instance lock, if any, is held and before the
// first Start, and cancels the subscription as it returns, after the stop phase, so a SIGHUP during
// start or shutdown never ends the process. Each SIGHUP Run receives while waiting begins one
// reload round; one received during the start phase begins a round once Run starts waiting, one
// received during shutdown is dropped, and any number received during a round begin one more. A
// round's failure, a Reload that returns an error, panics or overruns the round's deadline, is the
// failure that begins shutdown, as a start failure is; see WithReload.
//
// Run may be called once; a second call returns ErrAlreadyRan, even when the first returned
// without starting a Runner.
func (s *Supervisor) Run(ctx context.Context) error {
	if !s.ran.CompareAndSwap(false, true) {
		return ErrAlreadyRan
	}
	defer s.feed.setPhase(phaseStopped)

	s.log = s.logger
	if s.log == nil {
		s.log = slog.Default()
	}

	if s.instance != nil {
		in, err := AcquireInstance(s.instance.id, s.instance.opts)
		if err != nil {
			return err
		}
		// Deferred, so the lock is released only after the stop phase below has ended.
		defer in.Release()
	}

	// hangup stays nil without WithReload, so its case in wait never fires. Its capacity of one
	// coalesces the SIGHUPs that arrive while a round runs into one more round.
	var hangup chan os.Signal
	if s.reload != nil {
		hangup = make(chan os.Signal, 1)
		s.notify(hangup, syscall.SIGHUP)
		// Deferred, so the subscription is cancelled only after the stop phase below has ended.
		defer s.unnotify(hangup)
	}

	runs, failure, complete := s.start(ctx)
	if complete {
		s.feed.setPhase(phaseRunning)
		failure = s.wait(ctx, runs, hangup)
	}
	s.feed.setPhase(phaseStopping)

	stopCtx, cancel := s.newStopContext()
	defer cancel()

	errs := []error{failure}
	for _, l := range runs {
		errs = append(errs, l.isolated)
	}
	for _, l := range slices.Backward(runs) {
		errs = append(errs, l.stop(stopCtx, s.log)...)
	}
	return errors.Join(errs...)
}

// The messages of the records Run logs through the Supervisor's logger. Every record about a Runner
// names it under the attribute "runner"; the two records that end a reload round are about the
// round, not one Runner. Each error Run returns is logged once, as it is found: at
// Warn as msgStopTimedOut for a Stop that returns an error and for a Stop or Runner still pending
// when the grace period ends, and otherwise at Error, as msgPanicked when it carries a *PanicError
// and with the message of its event when not. A panic exit isolated under PanicIsolate is logged
// when it happens. Warn and Error records carry the
// error under the attribute "err". msgStarted and msgStopped, at Info, are logged for a Runner
// whose Start returned nil and for one whose Done then closed in the stop phase without a problem.
//
// A reload round ends with one record, or none when Run's context ends it: msgReloadRejected at
// Warn, with no "runner" and the load's error under "err", for a configuration the load failed to
// produce; msgReloadApplied at Info, with the names of the Runners whose Reload took it under
// "runners", once every Runner offered it has taken it; or the Error record of the Reload failure
// that begins shutdown, msgReloadFailed or msgPanicked, naming its Runner.
const (
	msgStarted        = "runner started"
	msgStopped        = "runner stopped"
	msgStartFailed    = "runner start failed"
	msgExited         = "runner exited"
	msgPanicked       = "runner panicked"
	msgStopTimedOut   = "runner stop timed out"
	msgReloadFailed   = "runner reload failed"
	msgReloadRejected = "reload rejected"
	msgReloadApplied  = "reload applied"
)

// logRunner logs msg at level about the Runner called name, with err when it is not nil.
func logRunner(ctx context.Context, log *slog.Logger, level slog.Level, msg, name string, err error) {
	if err == nil {
		log.LogAttrs(ctx, level, msg, slog.String("runner", name))
		return
	}
	log.LogAttrs(ctx, level, msg, slog.String("runner", name), slog.Any("err", err))
}

// logProblem logs err, a problem of the Runner called name, at Error: as msgPanicked when it
// carries a *PanicError, and with msg otherwise. It returns err.
func logProblem(ctx context.Context, log *slog.Logger, msg, name string, err error) error {
	var pe *PanicError
	if errors.As(err, &pe) {
		msg = msgPanicked
	}
	logRunner(ctx, log, slog.LevelError, msg, name, err)
	return err
}

// instanceLock names the instance lock a Supervisor holds while it runs.
type instanceLock struct {
	id   spec.Identity
	opts paths.Options
}

// start runs the start phase. It returns every Runner whose Start it called, in start order, the
// failure that ends the phase early, and whether every Runner started.
func (s *Supervisor) start(ctx context.Context) (runs []*launched, failure error, complete bool) {
	s.feed.setPhase(phaseStarting)
	startCtx, cancel := s.newStartContext(ctx)
	defer cancel()

	runs = make([]*launched, 0, len(s.runners))
	for _, r := range s.runners {
		if startCtx.Err() != nil {
			return runs, s.interrupted(ctx, r.Name(), ""), false
		}
		l := &launched{runner: r, name: r.Name(), state: starting, result: make(chan outcome, 1)}
		runs = append(runs, l)
		s.feed.setRunner(l.name, runnerStarting, nil)
		go func() { l.result <- guard(l.name, func() error { return r.Start(startCtx) }) }()

		select {
		case o := <-l.result:
			switch {
			case o.err == nil:
				l.running()
				s.feed.setRunner(l.name, runnerRunning, l.done)
				logRunner(ctx, s.log, slog.LevelInfo, msgStarted, l.name, nil)
			case !o.isPanic() && startCtx.Err() != nil && isContextErr(o.err):
				l.state = failed
				return runs, s.interrupted(ctx, l.name, l.name), false
			default:
				l.state = failed
				return runs, logProblem(ctx, s.log, msgStartFailed, l.name, l.wrap(o)), false
			}
		case <-startCtx.Done():
			return runs, s.interrupted(ctx, l.name, l.name), false
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

// interrupted returns interrupted(ctx, name) and logs the start timeout it may return as a start
// failure of the Runner called runner: the one whose Start was running or, when the phase ended
// between two Starts, the one whose Start is not called.
func (s *Supervisor) interrupted(ctx context.Context, runner, name string) error {
	err := interrupted(ctx, name)
	if err == nil {
		return nil
	}
	return logProblem(ctx, s.log, msgStartFailed, runner, err)
}

// isContextErr reports whether err only reports the end of a context.
func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// wait blocks until shutdown begins: ctx is done, a running Runner exits in a way that fails it,
// or a reload round fails. It returns that failure, or nil when ctx began it. A panic exit under
// PanicIsolate is recorded on its Runner and does not end the wait. Each value received on hangup
// runs one reload round; a nil hangup never does.
func (s *Supervisor) wait(ctx context.Context, runs []*launched, hangup <-chan os.Signal) error {
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
		case <-hangup:
			if err := s.reloadRound(ctx, runs); err != nil || ctx.Err() != nil {
				return err
			}
		case l := <-exited:
			l.exited = true
			o := guard(l.name, l.runner.Err)
			if s.policy == PanicIsolate && o.isPanic() {
				l.isolated = logProblem(ctx, s.log, msgExited, l.name, o.err)
				s.feed.setRunner(l.name, runnerIsolated, nil)
				continue
			}
			if o.err == nil {
				return logProblem(ctx, s.log, msgExited, l.name, fmt.Errorf("daemon: runner %q: %w", l.name, ErrRunnerExited))
			}
			return logProblem(ctx, s.log, msgExited, l.name, l.wrap(o))
		}
	}
}

// reloadRound loads the configuration once and offers it, in start order, to each Runner in runs
// that is still running and implements the configuration's Reloader. The load and every Reload
// share one context from newReloadContext, and each runs in its own goroutine that Run abandons
// when that context ends first. A load that fails, panics or is abandoned rejects the
// configuration and calls no Runner. reloadRound returns the failure of the first Reload that
// returns an error, panics or is abandoned, and offers it to no Runner after that one; it returns
// nil when every Runner offered the configuration took it and when ctx, the context passed to Run,
// ended the round. A Runner whose Reload is abandoned keeps its running state, so the stop phase
// stops it as any running Runner, and its Stop may run alongside the abandoned Reload.
func (s *Supervisor) reloadRound(ctx context.Context, runs []*launched) error {
	roundCtx, cancel := s.newReloadContext(ctx)
	defer cancel()

	offer, err := s.loadConfig(roundCtx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.LogAttrs(ctx, slog.LevelWarn, msgReloadRejected, slog.Any("err", err))
		}
		return nil
	}

	reloaded := make([]string, 0, len(runs))
	for _, l := range runs {
		if !l.reloadable() || !s.reload.accepts(l.runner) {
			continue
		}
		if roundCtx.Err() != nil {
			return s.reloadInterrupted(ctx, l.name, "")
		}
		result := make(chan outcome, 1)
		go func() {
			result <- guard(l.name, func() error {
				_, err := offer(roundCtx, l.runner)
				return err
			})
		}()

		select {
		case o := <-result:
			switch {
			case o.err == nil:
				reloaded = append(reloaded, l.name)
			case !o.isPanic() && roundCtx.Err() != nil && isContextErr(o.err):
				return s.reloadInterrupted(ctx, l.name, l.name)
			default:
				return logProblem(ctx, s.log, msgReloadFailed, l.name, l.wrap(o))
			}
		case <-roundCtx.Done():
			return s.reloadInterrupted(ctx, l.name, l.name)
		}
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, msgReloadApplied, slog.Any("runners", reloaded))
	return nil
}

// loadConfig calls the reload's load in its own goroutine and waits for it under ctx. A load that
// panics is reported as an error carrying the panic value; one still running when ctx is done is
// abandoned and reported with the context's error.
func (s *Supervisor) loadConfig(ctx context.Context) (offerFunc, error) {
	type loaded struct {
		offer offerFunc
		err   error
	}
	result := make(chan loaded, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				if cause, ok := v.(error); ok {
					result <- loaded{err: fmt.Errorf("daemon: reload load panicked: %w", cause)}
					return
				}
				result <- loaded{err: fmt.Errorf("daemon: reload load panicked: %v", v)}
			}
		}()
		offer, err := s.reload.load(ctx)
		if err != nil {
			err = fmt.Errorf("daemon: reload load: %w", err)
		}
		result <- loaded{offer: offer, err: err}
	}()

	select {
	case r := <-result:
		return r.offer, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("daemon: reload load: %w", ctx.Err())
	}
}

// reloadInterrupted returns what ends a reload round whose context is done: nil when ctx, the
// context passed to Run, is done, and otherwise ErrReloadTimeout, naming the Runner whose Reload
// was running when name is not empty. It logs the timeout as a reload failure of the Runner called
// runner: the one whose Reload was running or, when the round ended between two Reloads, the one
// whose Reload is not called.
func (s *Supervisor) reloadInterrupted(ctx context.Context, runner, name string) error {
	if ctx.Err() != nil {
		return nil
	}
	err := fmt.Errorf("daemon: %w", ErrReloadTimeout)
	if name != "" {
		err = fmt.Errorf("daemon: runner %q: %w", name, ErrReloadTimeout)
	}
	return logProblem(ctx, s.log, msgReloadFailed, runner, err)
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

// reloadable reports whether the Runner is running and has not exited, so it may be offered a
// reloaded configuration. A Runner whose Done has closed is not offered it even before wait has
// dealt with the exit.
func (l *launched) reloadable() bool {
	if l.state != running || l.exited {
		return false
	}
	select {
	case <-l.done:
		return false
	default:
		return true
	}
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
// it still calls Stop, but reports a timeout without waiting. It logs each problem it reports, and
// the late start and the stop of a Runner, through log.
func (l *launched) stop(ctx context.Context, log *slog.Logger) []error {
	stopped := make(chan outcome, 1)
	go func() { stopped <- guard(l.name, func() error { return l.runner.Stop(ctx) }) }()

	if ctx.Err() != nil {
		return []error{l.timedOut(ctx, log, l.timeout())}
	}
	select {
	case o := <-stopped:
		switch {
		case o.panicked:
			problem := logProblem(ctx, log, msgPanicked, l.name, o.err)
			return append([]error{problem}, l.finish(ctx, log, false)...)
		case o.err != nil:
			problem := fmt.Errorf("daemon: runner %q: %w: %w", l.name, ErrStopTimeout, o.err)
			return append([]error{l.timedOut(ctx, log, problem)}, l.finish(ctx, log, false)...)
		default:
			return l.finish(ctx, log, true)
		}
	case <-ctx.Done():
		return []error{l.timedOut(ctx, log, l.timeout())}
	}
}

// finish waits, under ctx, for a stopped Runner to finish and reports its problem: an exit Err
// other than the cancellation it was asked for, or a Start that returned late with an error other
// than the end of its context or panicked. A Runner whose late Start returned nil is waited for
// as a running one. A Runner still pending when ctx is done is reported as a timeout only when
// reportTimeout is set. It logs what it reports, and the late start and the stop of a Runner,
// through log.
func (l *launched) finish(ctx context.Context, log *slog.Logger, reportTimeout bool) []error {
	if l.state == starting {
		select {
		case o := <-l.result:
			if o.err != nil {
				l.state = failed
				if !o.isPanic() && isContextErr(o.err) {
					return nil
				}
				return []error{logProblem(ctx, log, msgStartFailed, l.name, l.wrap(o))}
			}
			l.running()
			logRunner(ctx, log, slog.LevelInfo, msgStarted, l.name, nil)
		case <-ctx.Done():
			return l.pending(ctx, log, reportTimeout)
		}
	}
	if l.state != running || l.exited {
		return nil
	}
	select {
	case <-l.done:
		o := guard(l.name, l.runner.Err)
		if o.err == nil || (!o.isPanic() && errors.Is(o.err, context.Canceled)) {
			logRunner(ctx, log, slog.LevelInfo, msgStopped, l.name, nil)
			return nil
		}
		return []error{logProblem(ctx, log, msgExited, l.name, l.wrap(o))}
	case <-ctx.Done():
		return l.pending(ctx, log, reportTimeout)
	}
}

// pending reports and logs a Runner still pending when the grace period ended, when reportTimeout
// is set.
func (l *launched) pending(ctx context.Context, log *slog.Logger, reportTimeout bool) []error {
	if reportTimeout {
		return []error{l.timedOut(ctx, log, l.timeout())}
	}
	return nil
}

// timedOut logs err, a stop problem of the Runner other than a panic, at Warn and returns it.
func (l *launched) timedOut(ctx context.Context, log *slog.Logger, err error) error {
	logRunner(ctx, log, slog.LevelWarn, msgStopTimedOut, l.name, err)
	return err
}

// timeout reports that the Runner did not finish stopping within the grace period.
func (l *launched) timeout() error {
	return fmt.Errorf("daemon: runner %q: %w", l.name, ErrStopTimeout)
}

// runPhase is how far Run has got, as the health checks see it.
type runPhase int

const (
	// phaseIdle is a Supervisor whose Run has not begun.
	phaseIdle runPhase = iota
	// phaseStarting is the start phase.
	phaseStarting
	// phaseRunning is every Start returned and shutdown not begun.
	phaseRunning
	// phaseStopping is shutdown begun and Run not returned.
	phaseStopping
	// phaseStopped is Run returned.
	phaseStopped
)

// runnerPhase is how far one Runner has got, as the health checks see it.
type runnerPhase int

const (
	// runnerIdle is a Runner whose Start has not been called.
	runnerIdle runnerPhase = iota
	// runnerStarting is a Runner whose Start has been called and has not returned nil. A Start
	// that fails leaves the Runner in this phase: shutdown begins next, and from then on the checks
	// report the phase of the run, not of the Runner.
	runnerStarting
	// runnerRunning is a Runner whose Start returned nil. It stays in this phase when it exits:
	// exitPhase reads a closed Done as runnerExited.
	runnerRunning
	// runnerExited is a running Runner whose Done has closed. Only exitPhase reports it.
	runnerExited
	// runnerIsolated is a Runner whose panic exit was isolated under PanicIsolate.
	runnerIsolated
)

// String describes the phase for a failing check.
func (p runnerPhase) String() string {
	switch p {
	case runnerIdle:
		return "not started"
	case runnerStarting:
		return "starting"
	case runnerRunning:
		return "running"
	case runnerExited:
		return "exited"
	default:
		return "exited with a panic and is isolated"
	}
}

// feed is the state of a run that the health checks read. Run's goroutine writes it at each
// transition and any goroutine may read it; mu guards every field. It is kept apart from launched,
// whose fields Run's goroutine alone reads and writes.
type feed struct {
	mu      sync.Mutex
	phase   runPhase
	runners []runnerFeed
	index   map[string]int
}

// runnerFeed is one Runner in a feed. done is the Runner's Done, set while it is running.
type runnerFeed struct {
	name  string
	phase runnerPhase
	done  <-chan struct{}
}

// track sets the feed up for runners, in start order.
func (f *feed) track(runners []Runner) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runners = make([]runnerFeed, len(runners))
	f.index = make(map[string]int, len(runners))
	for i, r := range runners {
		f.runners[i] = runnerFeed{name: r.Name()}
		f.index[r.Name()] = i
	}
}

// setPhase publishes the phase of the run.
func (f *feed) setPhase(p runPhase) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.phase = p
}

// setRunner publishes the phase of the Runner called name, with its Done while it is running.
func (f *feed) setRunner(name string, p runnerPhase, done <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := &f.runners[f.index[name]]
	r.phase, r.done = p, done
}

// live is the supervisor.live check: it passes from the beginning of the start phase until Run
// returns, an isolated panic included.
func (f *feed) live() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.phase {
	case phaseIdle:
		return errors.New("daemon: supervisor has not started")
	case phaseStopped:
		return errors.New("daemon: supervisor has stopped")
	default:
		return nil
	}
}

// ready is the supervisor.ready check: it passes once every Start has returned while no Runner
// has exited or been isolated and shutdown has not begun. A Runner whose Done has closed counts as
// exited.
func (f *feed) ready() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.phase {
	case phaseIdle:
		return errors.New("daemon: supervisor has not started")
	case phaseStarting:
		for _, r := range f.runners {
			if r.phase == runnerStarting {
				return fmt.Errorf("daemon: supervisor is starting runner %q", r.name)
			}
		}
		return errors.New("daemon: supervisor is starting")
	case phaseStopping:
		return errors.New("daemon: supervisor is stopping")
	case phaseStopped:
		return errors.New("daemon: supervisor has stopped")
	}
	for _, r := range f.runners {
		if r.phase != runnerRunning || closed(r.done) {
			return fmt.Errorf("daemon: runner %q %v", r.name, r.exitPhase())
		}
	}
	return nil
}

// running returns nil when the Runner called name is running and shutdown has not begun, and an
// error saying why it is not otherwise.
func (f *feed) running(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.phase {
	case phaseStopping:
		return fmt.Errorf("daemon: runner %q is not running: supervisor is stopping", name)
	case phaseStopped:
		return fmt.Errorf("daemon: runner %q is not running: supervisor has stopped", name)
	}
	r := f.runners[f.index[name]]
	if r.phase != runnerRunning || closed(r.done) {
		return fmt.Errorf("daemon: runner %q is not running: %v", name, r.exitPhase())
	}
	return nil
}

// exitPhase is the phase of r, taking a running Runner whose Done has closed as exited.
func (r runnerFeed) exitPhase() runnerPhase {
	if r.phase == runnerRunning && closed(r.done) {
		return runnerExited
	}
	return r.phase
}

// closed reports whether done has closed. A nil done never closes.
func closed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

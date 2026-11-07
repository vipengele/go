package daemontest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"go.vipengele.org/daemon"
)

var _ daemon.Runner = (*Runner)(nil)

// Runner is a scripted [daemon.Runner]. By default Start returns nil at once, the Runner then runs
// until [Runner.Fail] or Stop ends it, and Stop ends it cleanly, with an error wrapping
// context.Canceled. A [RunnerOption] makes Start fail or hang, or Stop fail or ignore its context.
// A Runner is safe for concurrent use.
//
// A Runner has an Order method, returning 0 unless the [Order] option sets another, which is what
// the Supervisor takes for a Runner without one.
type Runner struct {
	name  string
	order int

	startErr   error
	hangStart  bool
	stopErr    error
	ignoreStop bool

	// release is closed by Release and frees every hang.
	release     chan struct{}
	releaseOnce sync.Once

	started     chan struct{}
	startedOnce sync.Once
	stopped     chan struct{}
	stoppedOnce sync.Once

	done chan struct{}

	// mu guards the fields below.
	mu     sync.Mutex
	exited bool
	err    error
	calls  []string
}

// RunnerOption scripts a [Runner].
type RunnerOption func(*Runner)

// NewRunner returns a Runner named name, scripted by opts. The test's cleanup calls
// [Runner.Release], so no hang outlives the test.
func NewRunner(t testing.TB, name string, opts ...RunnerOption) *Runner {
	t.Helper()
	r := &Runner{
		name:    name,
		release: make(chan struct{}),
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(r)
	}
	t.Cleanup(r.Release)
	return r
}

// FailStart makes Start return err.
func FailStart(err error) RunnerOption {
	return func(r *Runner) { r.startErr = err }
}

// HangStart makes Start block until its context ends, returning the context's error, or until
// [Runner.Release], returning nil, so the Runner then counts as started.
func HangStart() RunnerOption {
	return func(r *Runner) { r.hangStart = true }
}

// IgnoreStop makes Stop block, ignoring its context, until [Runner.Release]. The Runner's Done does
// not close before then. Once released, Stop returns nil and the Runner exits cleanly.
func IgnoreStop() RunnerOption {
	return func(r *Runner) { r.ignoreStop = true }
}

// StopErr makes Stop return err. The Runner exits cleanly all the same.
func StopErr(err error) RunnerOption {
	return func(r *Runner) { r.stopErr = err }
}

// Order sets the Runner's Order to n.
func Order(n int) RunnerOption {
	return func(r *Runner) { r.order = n }
}

// Name returns the name the Runner was made with.
func (r *Runner) Name() string { return r.name }

// Order returns the Runner's place in the start order, 0 unless the [Order] option sets it.
func (r *Runner) Order() int { return r.order }

// Start records the call, then fails, hangs or returns nil as the Runner is scripted.
func (r *Runner) Start(ctx context.Context) error {
	r.record("start")
	if r.startErr != nil {
		return r.startErr
	}
	if r.hangStart {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.release:
		}
	}
	r.startedOnce.Do(func() { close(r.started) })
	return nil
}

// Stop records the call and closes the channel [Runner.Stopped] returns, then ends the Runner and
// returns the scripted error. With [IgnoreStop] it first blocks until [Runner.Release].
func (r *Runner) Stop(context.Context) error {
	r.record("stop")
	r.stoppedOnce.Do(func() { close(r.stopped) })
	if r.ignoreStop {
		<-r.release
	}
	r.exit(fmt.Errorf("daemontest: runner %q stopped: %w", r.name, context.Canceled))
	return r.stopErr
}

// Done is closed once the Runner has exited.
func (r *Runner) Done() <-chan struct{} { return r.done }

// Err returns why the Runner exited.
func (r *Runner) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Fail ends the Runner by itself with err, which [Runner.Err] then returns. Fail with
// [daemon.ErrRestartRequired] asks for a restart. It does nothing once the Runner has exited.
func (r *Runner) Fail(err error) { r.exit(err) }

// Started is closed once Start has returned nil.
func (r *Runner) Started() <-chan struct{} { return r.started }

// Stopped is closed as soon as Stop is called, before it returns, so it reports a Stop that
// [IgnoreStop] holds.
func (r *Runner) Stopped() <-chan struct{} { return r.stopped }

// Calls returns the names of the calls the Runner has received, "start" and "stop", in order.
func (r *Runner) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// Release frees every hang: a [HangStart] Start returns nil and an [IgnoreStop] Stop returns. It is
// safe to call more than once.
func (r *Runner) Release() {
	r.releaseOnce.Do(func() { close(r.release) })
}

func (r *Runner) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

// exit ends the Runner with err unless it has already exited.
func (r *Runner) exit(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exited {
		return
	}
	r.exited = true
	r.err = err
	close(r.done)
}

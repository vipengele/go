package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"go.vipengele.org/cli/spec/paths"
)

type reloadsInt struct {
	fakeRunner
	got []int
}

func (r *reloadsInt) Reload(_ context.Context, cfg int) error {
	r.got = append(r.got, cfg)
	return nil
}

type reloadsString struct {
	fakeRunner
	got []string
}

func (r *reloadsString) Reload(_ context.Context, cfg string) error {
	r.got = append(r.got, cfg)
	return nil
}

func TestNewRejectsANilReloadLoad(t *testing.T) {
	_, err := New([]Runner{&fakeRunner{name: "a"}}, WithReload[int](nil))
	if err == nil {
		t.Fatal("New with a nil load succeeded")
	}
}

func TestNewSetsTheReloadAndSignalSeams(t *testing.T) {
	s, err := New([]Runner{&fakeRunner{name: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.reload != nil {
		t.Error("reload is set without WithReload")
	}
	if s.newReloadContext == nil || s.notify == nil || s.unnotify == nil {
		t.Errorf("seams not defaulted: newReloadContext=%t notify=%t unnotify=%t",
			s.newReloadContext != nil, s.notify != nil, s.unnotify != nil)
	}
	ctx, cancel := s.newReloadContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Error("reload context has no deadline")
	}
}

func TestWithReloadLastOneWins(t *testing.T) {
	first := func(context.Context) (int, error) { return 1, nil }
	second := func(context.Context) (string, error) { return "two", nil }
	ints := &reloadsInt{fakeRunner: fakeRunner{name: "i"}}
	strs := &reloadsString{fakeRunner: fakeRunner{name: "s"}}
	plain := &fakeRunner{name: "p"}

	s, err := New([]Runner{ints, strs, plain}, WithReload(first), WithReload(second))
	if err != nil {
		t.Fatal(err)
	}
	if s.reload == nil {
		t.Fatal("reload not set")
	}
	offer, err := s.reload.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range []Runner{ints, strs, plain} {
		offered, err := offer(context.Background(), r)
		if err != nil {
			t.Fatalf("offer to %s: %v", r.Name(), err)
		}
		if want := r == Runner(strs); offered != want {
			t.Errorf("offered to %s = %t, want %t", r.Name(), offered, want)
		}
	}
	if len(ints.got) != 0 {
		t.Errorf("int Runner was offered %v", ints.got)
	}
	if len(strs.got) != 1 || strs.got[0] != "two" {
		t.Errorf("string Runner got %v, want [two]", strs.got)
	}
}

// reloadCall is one Reload a reloadRunner received: the Runner's name and the configuration.
type reloadCall[T any] struct {
	runner string
	cfg    T
}

// reloadLog is the ordered log of the Reload calls of the reloadRunners of one test that share a
// configuration type.
type reloadLog[T any] struct {
	mu    sync.Mutex
	calls []reloadCall[T]
}

func (l *reloadLog[T]) add(runner string, cfg T) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, reloadCall[T]{runner: runner, cfg: cfg})
}

func (l *reloadLog[T]) all() []reloadCall[T] {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

// reloadRunner is a scripted Runner that implements Reloader[T]. Reload records itself on log,
// reports its context on reloading and runs onReload; then it panics with panicValue when panics is
// set, and with hang blocks until the test frees the Runner, returning nil, or, with honourCtx,
// until its context is done, returning the context's error. Otherwise it returns err.
type reloadRunner[T any] struct {
	*scriptedRunner
	log       *reloadLog[T]
	reloading chan context.Context

	err        error
	panics     bool
	panicValue any
	hang       bool
	honourCtx  bool
	onReload   func()
}

func newReloading[T any](name string, log *reloadLog[T]) *reloadRunner[T] {
	return &reloadRunner[T]{scriptedRunner: newScripted(name), log: log, reloading: make(chan context.Context, 8)}
}

func (r *reloadRunner[T]) Reload(ctx context.Context, cfg T) error {
	r.log.add(r.name, cfg)
	r.reloading <- ctx
	if r.onReload != nil {
		r.onReload()
	}
	if r.panics {
		panic(r.panicValue)
	}
	if r.hang {
		if r.honourCtx {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-r.release:
				return nil
			}
		}
		<-r.release
	}
	return r.err
}

// counted is a load that counts its calls and returns cfg.
func counted[T any](calls *atomic.Int32, cfg T) func(context.Context) (T, error) {
	return func(context.Context) (T, error) {
		calls.Add(1)
		return cfg, nil
	}
}

// runReloading starts Run over list with opts and WithReload(load) and returns once every Runner
// has started and Run is waiting for shutdown, with the signal subscription Run made.
func runReloading[T any](t *testing.T, load func(context.Context) (T, error), opts []Option, list ...Runner) (*harness, subscription) {
	t.Helper()
	h := launchRun(t, append([]Option{WithReload(load)}, opts...), list...)
	sub := <-h.notified
	for _, r := range h.runners {
		h.startCtx = append(h.startCtx, <-r.started)
	}
	<-h.waiting
	return h, sub
}

// hangup delivers SIGHUP on the channel of sub as the signal package does, without blocking, and
// reports whether the channel took it.
func hangup(sub subscription) bool {
	select {
	case sub.ch <- syscall.SIGHUP:
		return true
	default:
		return false
	}
}

// checkStillRunning fails the test when Run has returned or called a Stop.
func checkStillRunning(t *testing.T, h *harness) {
	t.Helper()
	select {
	case err := <-h.result:
		t.Fatalf("Run returned %v, want it still running", err)
	default:
	}
	if got := h.rec.stopOrder(); len(got) != 0 {
		t.Errorf("Stop called on %v, want none", got)
	}
}

// roundRecords returns the records about a reload round as a whole.
func roundRecords(c *capturedLog) []logged {
	var out []logged
	for _, r := range c.all() {
		if r.msg == msgReloadRejected || r.msg == msgReloadApplied {
			out = append(out, r)
		}
	}
	return out
}

// recordsNamed returns the records with msg.
func recordsNamed(c *capturedLog, msg string) []logged {
	var out []logged
	for _, r := range c.all() {
		if r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

func checkCalls[T comparable](t *testing.T, got, want []reloadCall[T]) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("Reload calls = %v, want %v", got, want)
	}
}

func TestReloadOfAConfigTheLoadFailsToProduceCallsNoRunnerAndKeepsRunning(t *testing.T) {
	cause := errors.New("bad config")
	tests := []struct {
		name string
		// load is the load of the test; expire, when set, ends the round once load has been called.
		load   func(called chan<- struct{}, held <-chan struct{}) func(context.Context) (int, error)
		expire bool
		is     error
	}{
		{
			name: "error",
			load: func(called chan<- struct{}, _ <-chan struct{}) func(context.Context) (int, error) {
				return func(context.Context) (int, error) { called <- struct{}{}; return 0, cause }
			},
			is: cause,
		},
		{
			name: "overrun",
			load: func(called chan<- struct{}, held <-chan struct{}) func(context.Context) (int, error) {
				return func(context.Context) (int, error) { called <- struct{}{}; <-held; return 1, nil }
			},
			expire: true,
			is:     context.Canceled,
		},
		{
			name: "panic",
			load: func(called chan<- struct{}, _ <-chan struct{}) func(context.Context) (int, error) {
				return func(context.Context) (int, error) { called <- struct{}{}; panic(cause) }
			},
			is: cause,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called, held := make(chan struct{}, 1), make(chan struct{})
			capture, logger := newCapture()
			log := &reloadLog[int]{}
			a, b := newReloading("a", log), newReloading("b", log)
			h, sub := runReloading(t, tt.load(called, held), []Option{WithLogger(logger)}, a, b)

			hangup(sub)
			<-called
			if tt.expire {
				h.expireReload()
			}
			<-h.waiting
			checkStillRunning(t, h)
			checkCalls(t, log.all(), nil)

			got := roundRecords(capture)
			if len(got) != 1 {
				t.Fatalf("round records = %v, want one %q", got, msgReloadRejected)
			}
			if got[0].level != slog.LevelWarn || got[0].msg != msgReloadRejected {
				t.Errorf("round record = %v %q, want %v %q", got[0].level, got[0].msg, slog.LevelWarn, msgReloadRejected)
			}
			if r, ok := got[0].attrs["runner"]; ok {
				t.Errorf("rejection record names runner %v, want none", r)
			}
			if err, _ := got[0].attrs["err"].Any().(error); !errors.Is(err, tt.is) {
				t.Errorf("rejection err = %v, want it to wrap %v", err, tt.is)
			}

			h.cancelRun()
			if err := <-h.result; err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
			close(held)
			h.finish()
		})
	}
}

func TestReloadOffersAnAcceptedConfigInStartOrder(t *testing.T) {
	var cfg atomic.Int32
	cfg.Store(7)
	var loadCtx atomic.Value
	load := func(ctx context.Context) (int, error) {
		loadCtx.Store(ctx)
		return int(cfg.Load()), nil
	}
	capture, logger := newCapture()
	log := &reloadLog[int]{}
	a, b, c := newReloading("a", log), newReloading("b", log), newReloading("c", log)
	h, sub := runReloading(t, load, []Option{WithLogger(logger)}, a, b, newScripted("plain"), c)

	hangup(sub)
	<-h.waiting
	checkCalls(t, log.all(), []reloadCall[int]{{"a", 7}, {"b", 7}, {"c", 7}})
	ctx, _ := loadCtx.Load().(context.Context)
	if got := ctx.Value(ctxKey{}); got != "run" {
		t.Errorf("load context carries %v, want the run context's value", got)
	}
	if got := (<-b.reloading).Value(ctxKey{}); got != "run" {
		t.Errorf("Reload context carries %v, want the run context's value", got)
	}

	cfg.Store(8)
	hangup(sub)
	<-h.waiting
	checkCalls(t, log.all(), []reloadCall[int]{{"a", 7}, {"b", 7}, {"c", 7}, {"a", 8}, {"b", 8}, {"c", 8}})
	checkStillRunning(t, h)

	got := roundRecords(capture)
	if len(got) != 2 {
		t.Fatalf("round records = %v, want two %q", got, msgReloadApplied)
	}
	for _, r := range got {
		if r.level != slog.LevelInfo || r.msg != msgReloadApplied {
			t.Errorf("round record = %v %q, want %v %q", r.level, r.msg, slog.LevelInfo, msgReloadApplied)
		}
		if names, _ := r.attrs["runners"].Any().([]string); !slices.Equal(names, []string{"a", "b", "c"}) {
			t.Errorf("applied record runners = %v, want [a b c]", r.attrs["runners"])
		}
		if _, ok := r.attrs["err"]; ok {
			t.Errorf("applied record carries err %v", r.attrs["err"])
		}
	}

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	h.finish()
}

func TestReloadIsNotOfferedToARunnerOfAnotherTypeOrOneThatExited(t *testing.T) {
	ints, strs := &reloadLog[int]{}, &reloadLog[string]{}
	w, s := newReloading("w", ints), newReloading("s", strs)
	isolated, exiting := newReloading("isolated", ints), newReloading("exiting", ints)
	w.onReload = func() { exiting.exit(nil) }

	var loads atomic.Int32
	h, sub := runReloading(t, counted(&loads, 5), []Option{WithPanicPolicy(PanicIsolate)}, w, s, isolated, exiting)
	isolated.panicExit("isolated")
	<-h.waiting

	hangup(sub)
	err := <-h.result
	checkCalls(t, ints.all(), []reloadCall[int]{{"w", 5}})
	checkCalls(t, strs.all(), nil)
	checkJoined(t, err, []wantErr{{is: ErrRunnerExited}, {panicked: "isolated"}})
	if !strings.Contains(err.Error(), `runner "exiting"`) {
		t.Errorf("Run = %v, want it to name the exited Runner", err)
	}
	checkOrder(t, "stop", h.rec.stopOrder(), []string{"exiting", "isolated", "s", "w"})
	h.finish()
}

func TestReloadErrorEndsTheRunAndStopsEveryRunnerInReverse(t *testing.T) {
	cause := errors.New("cannot apply")
	capture, logger := newCapture()
	log := &reloadLog[int]{}
	a, b, c := newReloading("a", log), newReloading("b", log), newReloading("c", log)
	b.err = cause
	var loads atomic.Int32
	h, sub := runReloading(t, counted(&loads, 3), []Option{WithLogger(logger)}, a, b, c)

	hangup(sub)
	err := <-h.result
	checkJoined(t, err, []wantErr{{is: cause}})
	if !strings.Contains(err.Error(), `runner "b"`) {
		t.Errorf("Run = %v, want it to name runner b", err)
	}
	checkCalls(t, log.all(), []reloadCall[int]{{"a", 3}, {"b", 3}})
	checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})

	failed := recordsNamed(capture, msgReloadFailed)
	if len(failed) != 1 || failed[0].level != slog.LevelError || failed[0].attrs["runner"].String() != "b" {
		t.Errorf("reload failure records = %v, want one Error naming b", failed)
	}
	if got := roundRecords(capture); len(got) != 0 {
		t.Errorf("round records = %v, want none", got)
	}
	h.finish()
}

func TestReloadPanicEndsTheRunUnderEitherPolicy(t *testing.T) {
	for name, policy := range map[string]PanicPolicy{"fail": PanicFail, "isolate": PanicIsolate} {
		t.Run(name, func(t *testing.T) {
			capture, logger := newCapture()
			log := &reloadLog[int]{}
			a, b, c := newReloading("a", log), newReloading("b", log), newReloading("c", log)
			b.panics, b.panicValue = true, "oops"
			var loads atomic.Int32
			h, sub := runReloading(t, counted(&loads, 1), []Option{WithLogger(logger), WithPanicPolicy(policy)}, a, b, c)

			hangup(sub)
			err := <-h.result
			checkJoined(t, err, []wantErr{{panicked: "b"}})
			var pe *PanicError
			if errors.As(err, &pe) {
				if pe.Value != "oops" {
					t.Errorf("PanicError.Value = %v, want oops", pe.Value)
				}
				if !strings.Contains(string(pe.Stack), ").Reload") {
					t.Errorf("PanicError.Stack does not contain Reload:\n%s", pe.Stack)
				}
			}
			checkCalls(t, log.all(), []reloadCall[int]{{"a", 1}, {"b", 1}})
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			if got := recordsNamed(capture, msgPanicked); len(got) != 1 || got[0].attrs["runner"].String() != "b" {
				t.Errorf("panic records = %v, want one naming b", got)
			}
			h.finish()
		})
	}
}

func TestReloadOverrunEndsTheRunWithErrReloadTimeout(t *testing.T) {
	for _, honourCtx := range []bool{false, true} {
		name := "ignoring its context"
		if honourCtx {
			name = "returning its context's error"
		}
		t.Run(name, func(t *testing.T) {
			log := &reloadLog[int]{}
			a, b, c := newReloading("a", log), newReloading("b", log), newReloading("c", log)
			b.hang, b.honourCtx = true, honourCtx
			var loads atomic.Int32
			h, sub := runReloading(t, counted(&loads, 1), nil, a, b, c)

			hangup(sub)
			<-b.reloading
			h.expireReload()
			err := <-h.result
			checkJoined(t, err, []wantErr{{is: ErrReloadTimeout}})
			if !strings.Contains(err.Error(), `runner "b"`) {
				t.Errorf("Run = %v, want it to name runner b", err)
			}
			checkCalls(t, log.all(), []reloadCall[int]{{"a", 1}, {"b", 1}})
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			h.finish()
		})
	}
}

// expiresOnInspection is a reload round context that, once armed, is cancelled the first time its
// Err is read. Its Done stays open until then, so a Reload that arms it and returns nil is received
// as taken, and the round sees its deadline pass before the next Reload, never during this one.
type expiresOnInspection struct {
	context.Context
	cancel context.CancelFunc
	armed  *atomic.Bool
}

func (c expiresOnInspection) Err() error {
	if c.armed.Load() {
		c.cancel()
	}
	return c.Context.Err()
}

func TestReloadDeadlinePassingBetweenTwoReloadsIsAnUnnamedErrReloadTimeout(t *testing.T) {
	var sup *Supervisor
	capture := func(s *Supervisor) error { sup = s; return nil }
	logs, logger := newCapture()
	log := &reloadLog[int]{}
	a, b, c := newReloading("a", log), newReloading("b", log), newReloading("c", log)
	var armed atomic.Bool
	a.onReload = func() { armed.Store(true) }
	var loads atomic.Int32
	h, sub := runReloading(t, counted(&loads, 1), []Option{WithLogger(logger), capture}, a, b, c)

	// The round reads newReloadContext only after it receives the hangup sent below.
	harnessContext := sup.newReloadContext
	sup.newReloadContext = func(parent context.Context) (context.Context, context.CancelFunc) {
		ctx, cancel := harnessContext(parent)
		return expiresOnInspection{Context: ctx, cancel: cancel, armed: &armed}, cancel
	}

	hangup(sub)
	err := <-h.result
	checkJoined(t, err, []wantErr{{is: ErrReloadTimeout}})
	for _, name := range []string{"a", "b"} {
		if strings.Contains(err.Error(), `runner "`+name+`"`) {
			t.Errorf("Run = %v, want it to name no runner, not %s", err, name)
		}
	}
	checkCalls(t, log.all(), []reloadCall[int]{{"a", 1}})
	got := recordsNamed(logs, msgReloadFailed)
	if len(got) != 1 {
		t.Fatalf("%q records = %v, want one", msgReloadFailed, got)
	}
	if got[0].level != slog.LevelError {
		t.Errorf("%q level = %v, want %v", msgReloadFailed, got[0].level, slog.LevelError)
	}
	if r, ok := got[0].attrs["runner"]; !ok || r.String() != "b" {
		t.Errorf("%q runner = %v (present %t), want b", msgReloadFailed, r, ok)
	}
	if e, _ := got[0].attrs["err"].Any().(error); !errors.Is(e, ErrReloadTimeout) {
		t.Errorf("%q err = %v, want it to wrap ErrReloadTimeout", msgReloadFailed, e)
	}
	checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
	h.finish()
}

func TestReloadInterruptedByTheRunContextIsShutdown(t *testing.T) {
	for _, honourCtx := range []bool{false, true} {
		name := "ignoring its context"
		if honourCtx {
			name = "returning its context's error"
		}
		t.Run(name, func(t *testing.T) {
			log := &reloadLog[int]{}
			a, b, c := newReloading("a", log), newReloading("b", log), newReloading("c", log)
			b.hang, b.honourCtx = true, honourCtx
			var loads atomic.Int32
			h, sub := runReloading(t, counted(&loads, 1), nil, a, b, c)

			hangup(sub)
			<-b.reloading
			h.cancelRun()
			if err := <-h.result; err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"c", "b", "a"})
			h.finish()
		})
	}
}

func TestReloadTriggeredDuringStartIsAppliedOnceAfterStart(t *testing.T) {
	log := &reloadLog[int]{}
	a, b := newReloading("a", log), newReloading("b", log)
	a.hangStart = true
	var loads atomic.Int32
	h := launchRun(t, []Option{WithReload(counted(&loads, 4))}, a, b)

	sub := <-h.notified
	if len(sub.starts) != 0 {
		t.Errorf("subscribed after Start of %v, want before the first Start", sub.starts)
	}
	if !slices.Equal(sub.signals, []os.Signal{syscall.SIGHUP}) {
		t.Errorf("subscribed to %v, want SIGHUP only", sub.signals)
	}
	<-a.started
	hangup(sub)
	if n := loads.Load(); n != 0 {
		t.Errorf("load called %d times during start, want none", n)
	}
	a.free()
	<-b.started
	<-h.waiting
	<-h.waiting

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if n := loads.Load(); n != 1 {
		t.Errorf("load called %d times, want 1", n)
	}
	checkCalls(t, log.all(), []reloadCall[int]{{"a", 4}, {"b", 4}})
	h.finish()
}

func TestReloadTriggersDuringARoundCoalesceIntoOneMoreRound(t *testing.T) {
	log := &reloadLog[int]{}
	a := newReloading("a", log)
	a.hang = true
	var loads atomic.Int32
	h, sub := runReloading(t, counted(&loads, 2), nil, a)

	hangup(sub)
	<-a.reloading
	if !hangup(sub) {
		t.Fatal("first SIGHUP during the round was not taken")
	}
	hangup(sub)
	a.free()
	<-h.waiting
	<-h.waiting

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if n := loads.Load(); n != 2 {
		t.Errorf("load called %d times, want 2", n)
	}
	checkCalls(t, log.all(), []reloadCall[int]{{"a", 2}, {"a", 2}})
	h.finish()
}

func TestReloadTriggeredDuringShutdownIsDropped(t *testing.T) {
	log := &reloadLog[int]{}
	a, b := newReloading("a", log), newReloading("b", log)
	var loads atomic.Int32
	h, sub := runReloading(t, counted(&loads, 1), nil, a, b)
	b.onStop = func() { hangup(sub) }

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if n := loads.Load(); n != 0 {
		t.Errorf("load called %d times, want none", n)
	}
	checkCalls(t, log.all(), nil)
	if un := <-h.unnotified; un.ch != sub.ch {
		t.Error("unsubscribed another channel than the one subscribed")
	}
	h.finish()
}

func TestRunWithoutReloadSubscribesToNoSignal(t *testing.T) {
	h := runScripted(t, newScripted("a"))
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	select {
	case sub := <-h.notified:
		t.Errorf("Run subscribed to %v", sub.signals)
	default:
	}
	select {
	case <-h.unnotified:
		t.Error("Run cancelled a subscription it never made")
	default:
	}
	h.finish()
}

func TestRunCancelsTheReloadSubscriptionOnEveryReturn(t *testing.T) {
	load := func(context.Context) (int, error) { return 0, nil }

	t.Run("lock failure", func(t *testing.T) {
		opts := paths.Options{RuntimeDir: t.TempDir()}
		holder, err := AcquireInstance(lockIdentity(), opts)
		if err != nil {
			t.Fatalf("AcquireInstance: %v", err)
		}
		defer holder.Release()

		h := launchRun(t, []Option{WithReload(load), WithInstanceLock(lockIdentity(), opts)}, newScripted("a"))
		if err := <-h.result; !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("Run = %v, want ErrAlreadyRunning", err)
		}
		select {
		case sub := <-h.notified:
			t.Errorf("Run subscribed to %v without the instance lock", sub.signals)
		default:
		}
		select {
		case <-h.unnotified:
			t.Error("Run cancelled a subscription it never made")
		default:
		}
		h.finish()
	})

	t.Run("start failure", func(t *testing.T) {
		a, b := newScripted("a"), newScripted("b")
		b.startErr = errors.New("cannot start")
		h := launchRun(t, []Option{WithReload(load)}, a, b)
		sub := <-h.notified
		if err := <-h.result; err == nil {
			t.Fatal("Run = nil, want the start failure")
		}
		un := <-h.unnotified
		if un.ch != sub.ch {
			t.Error("unsubscribed another channel than the one subscribed")
		}
		checkOrder(t, "stop before unsubscribing", un.stops, []string{"b", "a"})
		h.finish()
	})

	t.Run("normal return", func(t *testing.T) {
		h, sub := runReloading(t, load, nil, newScripted("a"), newScripted("b"))
		h.cancelRun()
		if err := <-h.result; err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
		un := <-h.unnotified
		if un.ch != sub.ch {
			t.Error("unsubscribed another channel than the one subscribed")
		}
		checkOrder(t, "stop before unsubscribing", un.stops, []string{"b", "a"})
		h.finish()
	})
}

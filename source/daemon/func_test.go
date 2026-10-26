package daemon

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// funcRun runs a Supervisor over runners in the background. Each time Run starts waiting for
// shutdown with every Runner running, it sends on waiting.
type funcRun struct {
	cancel  context.CancelFunc
	waiting chan struct{}
	result  chan error
}

func startFuncRun(t *testing.T, ctx context.Context, opts []Option, runners ...Runner) *funcRun {
	t.Helper()
	s, err := New(runners, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fr := &funcRun{waiting: make(chan struct{}, 16), result: make(chan error, 1)}
	s.waiting = func() { fr.waiting <- struct{}{} }
	runCtx, cancel := context.WithCancel(ctx)
	fr.cancel = cancel
	go func() { fr.result <- s.Run(runCtx) }()
	t.Cleanup(cancel)
	return fr
}

// untilStopped is a run function that returns its context's error once that context is done.
func untilStopped(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestFuncCleanExitIsErrRunnerExited(t *testing.T) {
	baseline := runtime.NumGoroutine()
	run := startFuncRun(t, context.Background(), nil,
		Func("a", untilStopped),
		Func("b", func(context.Context) error { return nil }))

	err := <-run.result
	if !errors.Is(err, ErrRunnerExited) || !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("Run = %v, want ErrRunnerExited naming b", err)
	}
	run.cancel()
	settle(t, baseline)
}

func TestFuncErrorExitIsTheFailure(t *testing.T) {
	boom := errors.New("boom")
	run := startFuncRun(t, context.Background(), nil,
		Func("a", untilStopped),
		Func("b", func(context.Context) error { return boom }))

	err := <-run.result
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("Run = %v, want boom naming b", err)
	}
}

func TestFuncRunnerReportsItsExit(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		run  func(context.Context) error
		want error
	}{
		{name: "clean", run: func(context.Context) error { return nil }},
		{name: "error", run: func(context.Context) error { return boom }, want: boom},
		{name: "nil run", want: ErrNilFunc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Func("a", tt.run)
			if err := r.Err(); err != nil {
				t.Errorf("Err before Start = %v, want nil", err)
			}
			if err := r.Start(context.Background()); err != nil {
				t.Fatalf("Start = %v, want nil", err)
			}
			<-r.Done()
			if err := r.Err(); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Errorf("Err = %v, want %v", err, tt.want)
			}
			if err := r.Stop(context.Background()); err != nil {
				t.Errorf("Stop after exit = %v, want nil", err)
			}
		})
	}
}

func TestFuncPanicExitUnderPanicFailBeginsShutdown(t *testing.T) {
	cause := errors.New("cause")
	siblingStopped := make(chan struct{})
	run := startFuncRun(t, context.Background(), []Option{WithPanicPolicy(PanicFail)},
		Func("a", func(ctx context.Context) error {
			<-ctx.Done()
			close(siblingStopped)
			return ctx.Err()
		}),
		Func("b", func(context.Context) error { panic(cause) }))

	err := <-run.result
	var pe *PanicError
	if !errors.As(err, &pe) || pe.Runner != "b" || !errors.Is(err, cause) {
		t.Fatalf("Run = %v, want b's *PanicError wrapping the cause", err)
	}
	if !strings.Contains(string(pe.Stack), "TestFuncPanicExitUnderPanicFailBeginsShutdown") {
		t.Errorf("PanicError.Stack does not contain the panicking function:\n%s", pe.Stack)
	}
	<-siblingStopped
}

func TestFuncPanicExitUnderPanicIsolateKeepsTheSiblingsRunning(t *testing.T) {
	trigger := make(chan struct{})
	siblingCtx := make(chan context.Context, 1)
	run := startFuncRun(t, context.Background(), []Option{WithPanicPolicy(PanicIsolate)},
		Func("a", func(ctx context.Context) error {
			siblingCtx <- ctx
			<-ctx.Done()
			return ctx.Err()
		}),
		Func("b", func(context.Context) error {
			<-trigger
			panic("oops")
		}))

	<-run.waiting
	close(trigger)
	<-run.waiting
	if err := (<-siblingCtx).Err(); err != nil {
		t.Errorf("sibling's context after the isolated panic = %v, want not done", err)
	}

	run.cancel()
	err := <-run.result
	var pe *PanicError
	if !errors.As(err, &pe) || pe.Runner != "b" || pe.Value != "oops" {
		t.Fatalf("Run = %v, want b's isolated *PanicError", err)
	}
	if errs := joined(t, err); len(errs) != 1 {
		t.Errorf("Run joined %v, want only the isolated panic", errs)
	}
}

func TestFuncRunGetsTheRunValuesAndOutlivesTheStartPhase(t *testing.T) {
	type key struct{}
	runCtx := make(chan context.Context, 1)
	run := startFuncRun(t, context.WithValue(context.Background(), key{}, "run"), nil,
		Func("a", func(ctx context.Context) error {
			runCtx <- ctx
			<-ctx.Done()
			return ctx.Err()
		}))

	<-run.waiting
	ctx := <-runCtx
	if got := ctx.Value(key{}); got != "run" {
		t.Errorf("run's context carries %v, want the run context's value", got)
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("run's context once the start phase ended = %v, want not done", err)
	}

	run.cancel()
	if err := <-run.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("run's context after Run = %v, want cancelled", err)
	}
}

func TestFuncStopCancelsRunAndWaitsForIt(t *testing.T) {
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r := Func("a", func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		<-release
		close(returned)
		return ctx.Err()
	})
	startCtx, endStart := context.WithCancel(context.Background())
	if err := r.Start(startCtx); err != nil {
		t.Fatalf("Start = %v, want nil", err)
	}
	endStart()
	<-entered

	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop(context.Background()) }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned %v before run did", err)
	case <-r.Done():
		t.Fatalf("Done closed before run returned")
	default:
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Errorf("Stop = %v, want nil", err)
	}
	select {
	case <-returned:
	default:
		t.Errorf("Stop returned before run did")
	}
	if err := r.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("Err = %v, want context.Canceled", err)
	}
}

func TestFuncStopReturnsTheContextErrorWhenRunOutlastsIt(t *testing.T) {
	release := make(chan struct{})
	r := Func("a", func(context.Context) error {
		<-release
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Stop = %v, want context.Canceled", err)
	}
	close(release)
	<-r.Done()
}

func TestFuncStartTwiceAndStopBeforeStart(t *testing.T) {
	t.Run("start twice", func(t *testing.T) {
		calls := make(chan struct{}, 2)
		r := Func("a", func(ctx context.Context) error {
			calls <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
		if err := r.Start(context.Background()); err != nil {
			t.Fatalf("first Start = %v, want nil", err)
		}
		if err := r.Start(context.Background()); err == nil {
			t.Errorf("second Start = nil, want an error")
		}
		if err := r.Stop(context.Background()); err != nil {
			t.Errorf("Stop = %v, want nil", err)
		}
		if len(calls) != 1 {
			t.Errorf("run called %d times, want once", len(calls))
		}
	})
	t.Run("stop before start", func(t *testing.T) {
		called := make(chan struct{}, 1)
		r := Func("a", func(context.Context) error {
			called <- struct{}{}
			return nil
		})
		if err := r.Stop(context.Background()); err != nil {
			t.Errorf("Stop before Start = %v, want nil", err)
		}
		if err := r.Start(context.Background()); err == nil {
			t.Errorf("Start after Stop = nil, want an error")
		}
		select {
		case <-r.Done():
			t.Errorf("Done closed on a Runner never started")
		default:
		}
		if len(called) != 0 {
			t.Errorf("run called on a Runner stopped before Start")
		}
	})
}

func TestFuncOrderPlacesTheRunnerInTheStartOrder(t *testing.T) {
	if got := orderOf(Func("a", untilStopped)); got != 0 {
		t.Errorf("Order without WithOrder = %d, want 0", got)
	}
	if got := orderOf(Func("a", untilStopped, WithOrder(-2), nil)); got != -2 {
		t.Errorf("Order with WithOrder(-2) = %d, want -2", got)
	}

	s, err := New([]Runner{Func("late", untilStopped, WithOrder(1)), Func("early", untilStopped)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.runners[0].Name() != "early" || s.runners[1].Name() != "late" {
		t.Errorf("start order = %s, %s; want early, late", s.runners[0].Name(), s.runners[1].Name())
	}
}

func TestFuncBlankNameIsRejectedByNew(t *testing.T) {
	if _, err := New([]Runner{Func(" ", untilStopped)}); err == nil {
		t.Errorf("New accepted a Func Runner with a blank name")
	}
}

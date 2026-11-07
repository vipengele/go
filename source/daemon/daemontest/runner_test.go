package daemontest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"go.vipengele.org/daemon"
)

const (
	startTimeout = time.Minute
	grace        = 5 * time.Second
)

// run builds a Supervisor over rs on a fake clock and runs it in a goroutine. The returned channel
// receives Run's error.
func run(t *testing.T, ctx context.Context, clock *Clock, rs ...daemon.Runner) <-chan error {
	t.Helper()
	s, err := daemon.New(rs,
		daemon.WithClock(clock),
		daemon.WithStartTimeout(startTimeout),
		daemon.WithGrace(grace),
	)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return done
}

// result waits for Run's error.
func result(t *testing.T, ctx context.Context, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatal("Run did not return")
		return nil
	}
}

func wait(t *testing.T, ctx context.Context, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("%s did not happen", what)
	}
}

func setup(t *testing.T) (context.Context, *Clock) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx, NewClock(time.Unix(0, 0))
}

func TestFailStart(t *testing.T) {
	ctx, clock := setup(t)
	boom := errors.New("boom")
	r := NewRunner(t, "a", FailStart(boom))

	err := result(t, ctx, run(t, ctx, clock, r))
	if !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want an error wrapping the start error", err)
	}
	if got, want := r.Calls(), []string{"start", "stop"}; !slices.Equal(got, want) {
		t.Errorf("Calls = %v, want %v", got, want)
	}
}

func TestHangStartTimesOut(t *testing.T) {
	ctx, clock := setup(t)
	r := NewRunner(t, "a", HangStart())
	done := run(t, ctx, clock, r)

	if err := clock.BlockUntilTimers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(startTimeout)
	if err := result(t, ctx, done); !errors.Is(err, daemon.ErrStartTimeout) {
		t.Fatalf("Run = %v, want ErrStartTimeout", err)
	}
}

func TestIgnoreStopTimesOut(t *testing.T) {
	ctx, clock := setup(t)
	r := NewRunner(t, "a", IgnoreStop())
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := run(t, runCtx, clock, r)

	wait(t, ctx, r.Started(), "start")
	cancel()
	wait(t, ctx, r.Stopped(), "stop")
	if err := clock.BlockUntilTimers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(grace)
	if err := result(t, ctx, done); !errors.Is(err, daemon.ErrStopTimeout) {
		t.Fatalf("Run = %v, want ErrStopTimeout", err)
	}
}

func TestReleaseFreesIgnoredStop(t *testing.T) {
	ctx, clock := setup(t)
	r := NewRunner(t, "a", IgnoreStop())
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := run(t, runCtx, clock, r)

	wait(t, ctx, r.Started(), "start")
	cancel()
	wait(t, ctx, r.Stopped(), "stop")
	r.Release()
	r.Release()
	if err := result(t, ctx, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	wait(t, ctx, r.Done(), "exit")
}

func TestFailRestartRequired(t *testing.T) {
	ctx, clock := setup(t)
	r := NewRunner(t, "a")
	done := run(t, ctx, clock, r)

	wait(t, ctx, r.Started(), "start")
	r.Fail(daemon.ErrRestartRequired)
	err := result(t, ctx, done)
	if !daemon.RestartRequested(err) {
		t.Fatalf("Run = %v, want a requested restart", err)
	}
	if !errors.Is(r.Err(), daemon.ErrRestartRequired) {
		t.Errorf("Err = %v, want ErrRestartRequired", r.Err())
	}
}

func TestFailEndsRunnerWithErr(t *testing.T) {
	ctx, clock := setup(t)
	boom := errors.New("boom")
	r := NewRunner(t, "a")
	done := run(t, ctx, clock, r)

	wait(t, ctx, r.Started(), "start")
	r.Fail(boom)
	r.Fail(errors.New("later"))
	if err := result(t, ctx, done); !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want an error wrapping the failure", err)
	}
	if !errors.Is(r.Err(), boom) {
		t.Errorf("Err = %v, want the first failure", r.Err())
	}
}

func TestStopErr(t *testing.T) {
	ctx, clock := setup(t)
	boom := errors.New("boom")
	r := NewRunner(t, "a", StopErr(boom))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := run(t, runCtx, clock, r)

	wait(t, ctx, r.Started(), "start")
	cancel()
	if err := result(t, ctx, done); !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want an error wrapping the stop error", err)
	}
}

func TestCleanStop(t *testing.T) {
	ctx, clock := setup(t)
	r := NewRunner(t, "a")
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := run(t, runCtx, clock, r)

	wait(t, ctx, r.Started(), "start")
	cancel()
	if err := result(t, ctx, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if got, want := r.Calls(), []string{"start", "stop"}; !slices.Equal(got, want) {
		t.Errorf("Calls = %v, want %v", got, want)
	}
}

func TestOrderSortsRunners(t *testing.T) {
	ctx, clock := setup(t)
	boom := errors.New("boom")
	late := NewRunner(t, "late", Order(2))
	mid := NewRunner(t, "mid")
	early := NewRunner(t, "early", Order(-1), FailStart(boom))

	// The failing Runner is given last yet starts first, so no other Start is called.
	err := result(t, ctx, run(t, ctx, clock, late, mid, early))
	if !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want an error wrapping the start error", err)
	}
	if got := early.Calls(); !slices.Equal(got, []string{"start", "stop"}) {
		t.Errorf("early Calls = %v, want start and stop", got)
	}
	for _, r := range []*Runner{mid, late} {
		if got := r.Calls(); len(got) != 0 {
			t.Errorf("%s Calls = %v, want none", r.Name(), got)
		}
	}
}

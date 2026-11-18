package clock_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/commons/clock"
)

// waitLimit bounds every wait in these tests, so that a Sleep or an AfterFunc that never returns
// fails the test instead of hanging it.
const waitLimit = 10 * time.Second

// sleepResult runs Sleep in a goroutine and returns the channel its error arrives on.
func sleepResult(ctx context.Context, c clock.Clock, d time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() { done <- c.Sleep(ctx, d) }()
	return done
}

// await returns the error Sleep sent on done, failing the test when none arrives within waitLimit.
func await(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(waitLimit):
		t.Fatalf("Sleep did not return within %v", waitLimit)
		return nil
	}
}

func TestSystemNowFollowsTheWallClock(t *testing.T) {
	t.Parallel()

	before := time.Now()
	now := clock.System().Now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Fatalf("Now() = %v, want between %v and %v", now, before, after)
	}
}

func TestSystemSleepReturnsNilOnceTheDurationElapses(t *testing.T) {
	t.Parallel()

	const d = 5 * time.Millisecond
	start := time.Now()
	err := await(t, sleepResult(context.Background(), clock.System(), d))

	if err != nil {
		t.Fatalf("Sleep() = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed < d {
		t.Fatalf("Sleep returned after %v, want at least %v", elapsed, d)
	}
}

// endingContext reports no error from its first Err call and context.Canceled from every later
// one, with a Done channel that is already closed: the state a context is in when something
// cancels it between a check of Err and a wait on Done.
type endingContext struct {
	context.Context
	checked *atomic.Bool
}

func (c endingContext) Err() error {
	if c.checked.Swap(true) {
		return context.Canceled
	}
	return nil
}

func (endingContext) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func TestSystemSleepOfANonPositiveDurationNeverWaitsOnTheContext(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{0, -time.Nanosecond} {
		for range 1000 {
			ctx := endingContext{context.Background(), new(atomic.Bool)}
			if err := clock.System().Sleep(ctx, d); err != nil {
				t.Fatalf("Sleep(%v) = %v, want nil", d, err)
			}
		}
	}
}

func TestSystemSleepReturnsAtOnceForANonPositiveDuration(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{0, -time.Nanosecond, -time.Hour} {
		if err := await(t, sleepResult(context.Background(), clock.System(), d)); err != nil {
			t.Errorf("Sleep(%v) = %v, want nil", d, err)
		}
	}
}

func TestSystemSleepReturnsTheErrorOfAContextThatHasAlreadyEnded(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Unix(0, 0))
	t.Cleanup(cancelExpired)

	tests := []struct {
		name string
		ctx  context.Context
		d    time.Duration
		want error
	}{
		{"canceled", canceled, time.Hour, context.Canceled},
		{"canceled with a non-positive duration", canceled, 0, context.Canceled},
		{"deadline exceeded", expired, time.Hour, context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := await(t, sleepResult(tt.ctx, clock.System(), tt.d)); !errors.Is(err, tt.want) {
				t.Fatalf("Sleep() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSystemSleepReturnsWhenTheContextIsCanceledDuringIt(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := sleepResult(ctx, clock.System(), time.Hour)

	select {
	case err := <-done:
		t.Fatalf("Sleep returned %v before the context was canceled", err)
	case <-time.After(5 * time.Millisecond):
	}
	cancel()

	if err := await(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() = %v, want %v", err, context.Canceled)
	}
}

func TestSystemSleepReturnsWhenTheContextDeadlinePasses(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	t.Cleanup(cancel)

	if err := await(t, sleepResult(ctx, clock.System(), time.Hour)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Sleep() = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestSystemSleepEndedByItsContextLeavesNoWorkBehind(t *testing.T) {
	t.Parallel()

	// Each Sleep waits on an hour-long timer; one that kept it after returning would keep a timer
	// per iteration, and one that kept waiting would never return within waitLimit.
	for range 1000 {
		ctx, cancel := context.WithCancel(context.Background())
		done := sleepResult(ctx, clock.System(), time.Hour)
		cancel()
		if err := await(t, done); !errors.Is(err, context.Canceled) {
			t.Fatalf("Sleep() = %v, want %v", err, context.Canceled)
		}
	}
}

func TestSystemAfterFuncRunsFOnceTheDurationElapses(t *testing.T) {
	t.Parallel()

	ran := make(chan struct{})
	stop := clock.System().AfterFunc(time.Millisecond, func() { close(ran) })

	select {
	case <-ran:
	case <-time.After(waitLimit):
		t.Fatalf("f did not run within %v", waitLimit)
	}
	if stop() {
		t.Fatal("stop() after f ran = true, want false")
	}
}

func TestSystemAfterFuncStopPreventsF(t *testing.T) {
	t.Parallel()

	ran := make(chan struct{})
	stop := clock.System().AfterFunc(time.Hour, func() { close(ran) })

	if !stop() {
		t.Fatal("stop() before f ran = false, want true")
	}
	if stop() {
		t.Fatal("second stop() = true, want false")
	}
	select {
	case <-ran:
		t.Fatal("f ran after stop prevented it")
	default:
	}
}

func TestSystemAfterFuncRunsFInItsOwnGoroutine(t *testing.T) {
	t.Parallel()

	// f blocks until AfterFunc has returned, which deadlocks when AfterFunc runs f itself.
	returned := make(chan struct{})
	ran := make(chan struct{})
	clock.System().AfterFunc(0, func() {
		<-returned
		close(ran)
	})
	close(returned)

	select {
	case <-ran:
	case <-time.After(waitLimit):
		t.Fatalf("f did not run within %v", waitLimit)
	}
}

// otherClock is a Clock that is not the system clock.
type otherClock struct{}

func (otherClock) Now() time.Time                              { return time.Time{} }
func (otherClock) Sleep(context.Context, time.Duration) error  { return nil }
func (otherClock) AfterFunc(time.Duration, func()) func() bool { return func() bool { return false } }

func TestIsSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		clock clock.Clock
		want  bool
	}{
		{"System()", clock.System(), true},
		{"another implementation", otherClock{}, false},
		{"another implementation by pointer", &otherClock{}, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := clock.IsSystem(tt.clock); got != tt.want {
				t.Fatalf("IsSystem(%v) = %v, want %v", tt.clock, got, tt.want)
			}
		})
	}
}

func TestSystemReturnsEqualValues(t *testing.T) {
	t.Parallel()

	if clock.System() != clock.System() {
		t.Fatal("System() != System(), want every call to return an equal clock")
	}
}

package clocktest_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/commons/clock"
	"go.vipengele.org/commons/clock/clocktest"
)

// waitLimit bounds every wait in these tests, so that a Sleep that never returns or a Pending
// count that is never reached fails the test instead of hanging it.
const waitLimit = 10 * time.Second

func start() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

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

// awaitPending waits until c has n timers and sleeps pending, failing the test when it does not
// reach n within waitLimit.
func awaitPending(t *testing.T, c *clocktest.Manual, n int) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for c.Pending() != n {
		if time.Now().After(deadline) {
			t.Fatalf("Pending() = %d after %v, want %d", c.Pending(), waitLimit, n)
		}
		runtime.Gosched()
	}
}

// assertNotReturned fails the test when Sleep has already sent its error on done.
func assertNotReturned(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("Sleep returned %v before the clock reached its deadline", err)
	default:
	}
}

func TestNowReadsTheStartUntilAdvanced(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	if got := c.Now(); !got.Equal(start()) {
		t.Fatalf("Now() = %v, want %v", got, start())
	}
	c.Advance(90 * time.Second)
	if got, want := c.Now(), start().Add(90*time.Second); !got.Equal(want) {
		t.Fatalf("Now() after Advance = %v, want %v", got, want)
	}
}

func TestIsSystemReportsFalse(t *testing.T) {
	t.Parallel()

	if clock.IsSystem(clocktest.New(start())) {
		t.Fatal("IsSystem(New()) = true, want false")
	}
}

func TestAdvanceWithNothingPendingMovesTheClock(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	c.Advance(0)
	c.Advance(time.Minute)
	if got, want := c.Now(), start().Add(time.Minute); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want 0", got)
	}
}

func TestAdvancePanicsOnANegativeDuration(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	defer func() {
		if recover() == nil {
			t.Fatal("Advance(-1ns) did not panic")
		}
		if got := c.Now(); !got.Equal(start()) {
			t.Fatalf("Now() after the panic = %v, want %v", got, start())
		}
	}()
	c.Advance(-time.Nanosecond)
}

func TestAdvanceFiresTimersInDeadlineOrderAndTiesInRegistrationOrder(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	var got []string
	var at []time.Duration
	record := func(name string) func() {
		return func() {
			got = append(got, name)
			at = append(at, c.Now().Sub(start()))
		}
	}
	c.AfterFunc(3*time.Second, record("c"))
	c.AfterFunc(time.Second, record("a1"))
	c.AfterFunc(2*time.Second, record("b"))
	c.AfterFunc(time.Second, record("a2"))
	c.AfterFunc(time.Second, record("a3"))
	c.AfterFunc(4*time.Second, record("late"))

	c.Advance(3 * time.Second)

	if want := []string{"a1", "a2", "a3", "b", "c"}; !slices.Equal(got, want) {
		t.Fatalf("fired %v, want %v", got, want)
	}
	if want := []time.Duration{time.Second, time.Second, time.Second, 2 * time.Second, 3 * time.Second}; !slices.Equal(at, want) {
		t.Fatalf("Now() inside f = %v after start, want %v", at, want)
	}
	if got := c.Pending(); got != 1 {
		t.Fatalf("Pending() = %d, want 1", got)
	}
}

func TestAdvanceInterleavesTimersAndSleepsInDeadlineOrder(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	done := sleepResult(context.Background(), c, 2*time.Second)
	awaitPending(t, c, 1)

	// The sleep is due at 2s; a timer due at 3s observes it woken only if the sleep fired first.
	var woken bool
	c.AfterFunc(3*time.Second, func() {
		select {
		case <-done:
			woken = true
		case <-time.After(waitLimit):
		}
	})
	c.Advance(3 * time.Second)

	if !woken {
		t.Fatal("the timer due at 3s ran before the sleep due at 2s was woken")
	}
}

func TestATimerScheduledWithinTheWindowFiresInTheSameAdvance(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	var fired []time.Duration
	c.AfterFunc(time.Second, func() {
		fired = append(fired, c.Now().Sub(start()))
		c.AfterFunc(time.Second, func() {
			fired = append(fired, c.Now().Sub(start()))
			c.AfterFunc(time.Second, func() { fired = append(fired, c.Now().Sub(start())) })
		})
	})

	c.Advance(2500 * time.Millisecond)

	if want := []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(fired, want) {
		t.Fatalf("fired at %v, want %v", fired, want)
	}
	if got, want := c.Now(), start().Add(2500*time.Millisecond); !got.Equal(want) {
		t.Fatalf("Now() after Advance = %v, want %v", got, want)
	}
	if got := c.Pending(); got != 1 {
		t.Fatalf("Pending() = %d, want the timer due at 3s", got)
	}

	c.Advance(500 * time.Millisecond)
	if want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}; !slices.Equal(fired, want) {
		t.Fatalf("fired at %v, want %v", fired, want)
	}
}

func TestAfterFuncWithANonPositiveDurationFiresOnTheNextAdvance(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{0, -time.Hour} {
		c := clocktest.New(start())
		var ran bool
		c.AfterFunc(d, func() { ran = true })
		if ran {
			t.Fatalf("AfterFunc(%v) ran f before Advance", d)
		}
		if got := c.Pending(); got != 1 {
			t.Fatalf("AfterFunc(%v): Pending() = %d, want 1", d, got)
		}

		c.Advance(0)
		if !ran {
			t.Fatalf("AfterFunc(%v) did not run f on Advance(0)", d)
		}
		if got := c.Now(); !got.Equal(start()) {
			t.Fatalf("AfterFunc(%v): Now() = %v, want %v", d, got, start())
		}
	}
}

func TestStopBeforeTheTimerFiresPreventsF(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	var ran bool
	stop := c.AfterFunc(time.Second, func() { ran = true })

	if !stop() {
		t.Fatal("stop() before f ran = false, want true")
	}
	if stop() {
		t.Fatal("second stop() = true, want false")
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() after stop = %d, want 0", got)
	}
	c.Advance(time.Hour)
	if ran {
		t.Fatal("f ran after stop prevented it")
	}
}

func TestStopAfterTheTimerFiresReportsFalse(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	var runs int
	stop := c.AfterFunc(time.Second, func() { runs++ })
	c.Advance(time.Second)

	if stop() {
		t.Fatal("stop() after f ran = true, want false")
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() after f ran = %d, want 0", got)
	}
	c.Advance(time.Hour)
	if runs != 1 {
		t.Fatalf("f ran %d times, want 1", runs)
	}
}

func TestATimerStoppedByAnEarlierTimerInTheSameAdvanceDoesNotFire(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	var ran, stopped bool
	var stopLater func() bool
	c.AfterFunc(time.Second, func() { stopped = stopLater() })
	stopLater = c.AfterFunc(2*time.Second, func() { ran = true })

	c.Advance(time.Hour)

	if !stopped {
		t.Fatal("stop() from inside an earlier f = false, want true")
	}
	if ran {
		t.Fatal("f ran after an earlier f stopped it")
	}
}

func TestSleepReturnsNilOnceAdvancedToItsDeadline(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	done := sleepResult(context.Background(), c, time.Minute)
	awaitPending(t, c, 1)

	c.Advance(59 * time.Second)
	assertNotReturned(t, done)
	if got := c.Pending(); got != 1 {
		t.Fatalf("Pending() before the deadline = %d, want 1", got)
	}

	c.Advance(time.Second)
	if err := await(t, done); err != nil {
		t.Fatalf("Sleep() = %v, want nil", err)
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() after the sleep woke = %d, want 0", got)
	}
}

func TestSleepReturnsAtOnceForANonPositiveDuration(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	for _, d := range []time.Duration{0, -time.Nanosecond, -time.Hour} {
		if err := await(t, sleepResult(context.Background(), c, d)); err != nil {
			t.Errorf("Sleep(%v) = %v, want nil", d, err)
		}
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want 0", got)
	}
}

func TestSleepReturnsTheErrorOfAContextThatHasAlreadyEnded(t *testing.T) {
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

			c := clocktest.New(start())
			if err := await(t, sleepResult(tt.ctx, c, tt.d)); !errors.Is(err, tt.want) {
				t.Fatalf("Sleep() = %v, want %v", err, tt.want)
			}
			if got := c.Pending(); got != 0 {
				t.Fatalf("Pending() = %d, want 0", got)
			}
		})
	}
}

func TestSleepReturnsWhenTheContextIsCanceledDuringIt(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := sleepResult(ctx, c, time.Hour)
	awaitPending(t, c, 1)

	cancel()

	if err := await(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() = %v, want %v", err, context.Canceled)
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() after the context ended = %d, want 0", got)
	}
}

func TestPendingCountsEveryWaitingTimerAndSleep(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	first := sleepResult(ctx, c, time.Second)
	second := sleepResult(context.Background(), c, 2*time.Second)
	awaitPending(t, c, 2)
	stop := c.AfterFunc(3*time.Second, func() {})
	c.AfterFunc(4*time.Second, func() {})
	if got := c.Pending(); got != 4 {
		t.Fatalf("Pending() = %d, want 4", got)
	}

	cancel()
	if err := await(t, first); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Sleep() = %v, want %v", err, context.Canceled)
	}
	stop()
	if got := c.Pending(); got != 2 {
		t.Fatalf("Pending() after a cancel and a stop = %d, want 2", got)
	}

	c.Advance(4 * time.Second)
	if err := await(t, second); err != nil {
		t.Fatalf("second Sleep() = %v, want nil", err)
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() after Advance = %d, want 0", got)
	}
}

func TestATestWaitsForASleepToRegisterBeforeAdvancing(t *testing.T) {
	t.Parallel()

	c := clocktest.New(start())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// The goroutine sleeps a second between ticks; the test advances only once it is asleep.
	ticks := make(chan time.Time)
	go func() {
		for {
			if err := c.Sleep(ctx, time.Second); err != nil {
				close(ticks)
				return
			}
			ticks <- c.Now()
		}
	}()

	for i := 1; i <= 3; i++ {
		awaitPending(t, c, 1)
		c.Advance(time.Second)
		select {
		case got := <-ticks:
			if want := start().Add(time.Duration(i) * time.Second); !got.Equal(want) {
				t.Fatalf("tick %d at %v, want %v", i, got, want)
			}
		case <-time.After(waitLimit):
			t.Fatalf("tick %d did not arrive within %v", i, waitLimit)
		}
	}

	awaitPending(t, c, 1)
	cancel()
	select {
	case _, ok := <-ticks:
		if ok {
			t.Fatal("a tick arrived after the context was canceled")
		}
	case <-time.After(waitLimit):
		t.Fatalf("the sleeping goroutine did not stop within %v", waitLimit)
	}
}

func TestConcurrentAdvanceStopAndCancel(t *testing.T) {
	t.Parallel()

	const (
		timers   = 200
		sleepers = 200
		steps    = 50
	)
	c := clocktest.New(start())

	var wg sync.WaitGroup
	runs := make([]atomic.Int32, timers)
	stopped := make([]atomic.Bool, timers)
	for i := range timers {
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(i), 1))
			stop := c.AfterFunc(time.Duration(r.IntN(steps))*time.Millisecond, func() { runs[i].Add(1) })
			if r.IntN(2) == 0 {
				runtime.Gosched()
				stopped[i].Store(stop())
			}
		})
	}

	errs := make([]error, sleepers)
	for i := range sleepers {
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(i), 2))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if r.IntN(2) == 0 {
				go func() {
					runtime.Gosched()
					cancel()
				}()
			}
			errs[i] = c.Sleep(ctx, time.Duration(r.IntN(steps)+1)*time.Millisecond)
		})
	}

	advancers := make(chan struct{})
	for range 4 {
		go func() {
			defer func() { advancers <- struct{}{} }()
			for range steps {
				c.Advance(time.Millisecond)
				_ = c.Pending()
				_ = c.Now()
				runtime.Gosched()
			}
		}()
	}
	for range 4 {
		<-advancers
	}

	// Every sleep and timer is due by now, but some registered after the last Advance.
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	deadline := time.After(waitLimit)
	for done := false; !done; {
		c.Advance(time.Duration(steps) * time.Millisecond)
		select {
		case <-finished:
			done = true
		case <-deadline:
			t.Fatalf("goroutines did not finish within %v", waitLimit)
		default:
			runtime.Gosched()
		}
	}
	c.Advance(time.Duration(steps) * time.Millisecond)

	for i := range timers {
		n, s := runs[i].Load(), stopped[i].Load()
		if s && n != 0 || !s && n != 1 {
			t.Errorf("timer %d: ran %d times with stop() = %v, want exactly one of them", i, n, s)
		}
	}
	for i, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("sleeper %d: Sleep() = %v, want nil or %v", i, err, context.Canceled)
		}
	}
	if got := c.Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want 0", got)
	}
	if got, want := c.Now(), start().Add(time.Duration(4*steps)*time.Millisecond); got.Before(want) {
		t.Fatalf("Now() = %v, want at least %v", got, want)
	}
}

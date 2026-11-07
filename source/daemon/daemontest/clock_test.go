package daemontest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// fired returns a callback, a function that reports whether it has run, and a channel that
// closes when it runs.
func fired() (f func(), ran func() bool, done <-chan struct{}) {
	ch := make(chan struct{})
	var once sync.Once
	f = func() { once.Do(func() { close(ch) }) }
	ran = func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}
	return f, ran, ch
}

func waitFired(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not run")
	}
}

func TestNowAdvances(t *testing.T) {
	c := NewClock(epoch())
	if got := c.Now(); !got.Equal(epoch()) {
		t.Fatalf("Now() = %v, want %v", got, epoch())
	}
	c.Advance(3 * time.Second)
	c.Advance(0)
	if got, want := c.Now(), epoch().Add(3*time.Second); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}

func TestAdvanceNegativePanics(t *testing.T) {
	c := NewClock(epoch())
	defer func() {
		if recover() == nil {
			t.Fatal("Advance(-1) did not panic")
		}
		if got := c.Now(); !got.Equal(epoch()) {
			t.Fatalf("Now() = %v after a panicking Advance, want %v", got, epoch())
		}
	}()
	c.Advance(-1)
}

func TestTimerFiresOnlyAtItsDeadline(t *testing.T) {
	c := NewClock(epoch())
	f, ran, done := fired()
	c.AfterFunc(2*time.Second, f)

	c.Advance(time.Second)
	if c.BlockUntilTimers(canceled(), 1) != nil {
		t.Fatal("timer disarmed before its deadline")
	}
	if ran() {
		t.Fatal("callback ran before its deadline")
	}
	c.Advance(time.Second)
	waitFired(t, done)
}

// The firing order within one Advance is checked through advance, which hands back the callbacks
// in the order Advance starts them, so the test runs them in that order without a scheduler race.
func TestAdvanceFiresInDeadlineOrderAndTiesInArmOrder(t *testing.T) {
	c := NewClock(epoch())
	var order []string
	arm := func(d time.Duration, name string) {
		c.AfterFunc(d, func() { order = append(order, name) })
	}
	arm(3*time.Second, "3s")
	arm(time.Second, "1s-a")
	arm(2*time.Second, "2s")
	arm(time.Second, "1s-b")
	arm(4*time.Second, "4s")

	for _, f := range c.advance(3 * time.Second) {
		f()
	}
	want := []string{"1s-a", "1s-b", "2s", "3s"}
	if !slices.Equal(order, want) {
		t.Fatalf("fired %v, want %v", order, want)
	}
	if c.BlockUntilTimers(canceled(), 1) != nil {
		t.Fatal("the 4s timer is not armed")
	}
	if c.BlockUntilTimers(canceled(), 2) == nil {
		t.Fatal("a fired timer is still armed")
	}
}

// The timers alternate between two deadlines, enough of them that the sort partitions rather than
// running an insertion sort, so only the arm order recorded in each timer keeps ties in arm order.
func TestAdvanceFiresTimersWithTheSameDeadlineInArmOrder(t *testing.T) {
	c := NewClock(epoch())
	var order []string
	var early, late []string
	for i := range 64 {
		d, ties := 2*time.Second, &late
		if i%2 == 1 {
			d, ties = time.Second, &early
		}
		name := fmt.Sprintf("%v-%d", d, i)
		*ties = append(*ties, name)
		c.AfterFunc(d, func() { order = append(order, name) })
	}

	for _, f := range c.advance(2 * time.Second) {
		f()
	}
	if want := slices.Concat(early, late); !slices.Equal(order, want) {
		t.Fatalf("fired %v, want %v", order, want)
	}
}

func TestAdvanceStepwiseFiresInDeadlineOrder(t *testing.T) {
	c := NewClock(epoch())
	got := make(chan string, 3)
	c.AfterFunc(3*time.Second, func() { got <- "3s" })
	c.AfterFunc(time.Second, func() { got <- "1s" })
	c.AfterFunc(2*time.Second, func() { got <- "2s" })

	for _, want := range []string{"1s", "2s", "3s"} {
		c.Advance(time.Second)
		select {
		case name := <-got:
			if name != want {
				t.Fatalf("fired %q, want %q", name, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s timer did not fire", want)
		}
	}
}

func TestAdvancePastEveryDeadlineFiresThemAll(t *testing.T) {
	c := NewClock(epoch())
	var wg sync.WaitGroup
	for _, d := range []time.Duration{3 * time.Second, time.Second, 2 * time.Second} {
		wg.Add(1)
		c.AfterFunc(d, wg.Done)
	}
	c.Advance(10 * time.Second)
	wg.Wait()
}

func TestStopBeforeFire(t *testing.T) {
	c := NewClock(epoch())
	f, ran, _ := fired()
	stop := c.AfterFunc(time.Second, f)

	if !stop() {
		t.Fatal("stop() = false on an armed timer, want true")
	}
	if stop() {
		t.Fatal("second stop() = true, want false")
	}
	if fs := c.advance(time.Hour); len(fs) != 0 {
		t.Fatalf("advance past a stopped timer returned %d callbacks, want 0", len(fs))
	}
	if ran() {
		t.Fatal("stopped timer fired")
	}
}

func TestStopAfterFire(t *testing.T) {
	c := NewClock(epoch())
	f, _, done := fired()
	stop := c.AfterFunc(time.Second, f)
	c.Advance(time.Second)
	waitFired(t, done)
	if stop() {
		t.Fatal("stop() = true after the timer fired, want false")
	}
}

func TestNonPositiveDurationFiresOnNextAdvance(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		t.Run(d.String(), func(t *testing.T) {
			c := NewClock(epoch())
			f, ran, done := fired()
			c.AfterFunc(d, f)
			if ran() {
				t.Fatal("callback ran inside AfterFunc")
			}
			if c.BlockUntilTimers(canceled(), 1) != nil {
				t.Fatal("timer is not armed before Advance")
			}
			c.Advance(0)
			waitFired(t, done)
		})
	}
}

func TestBlockUntilTimersWaitsForArming(t *testing.T) {
	c := NewClock(epoch())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := make(chan error, 1)
	go func() { result <- c.BlockUntilTimers(ctx, 2) }()

	c.AfterFunc(time.Second, func() {})
	go c.AfterFunc(time.Second, func() {})

	if err := <-result; err != nil {
		t.Fatalf("BlockUntilTimers() = %v, want nil", err)
	}
}

func TestBlockUntilTimersReturnsAtOnceWhenArmed(t *testing.T) {
	c := NewClock(epoch())
	c.AfterFunc(time.Second, func() {})
	if err := c.BlockUntilTimers(canceled(), 1); err != nil {
		t.Fatalf("BlockUntilTimers() = %v, want nil", err)
	}
	if err := c.BlockUntilTimers(canceled(), 0); err != nil {
		t.Fatalf("BlockUntilTimers(0) = %v, want nil", err)
	}
}

func TestBlockUntilTimersReturnsContextError(t *testing.T) {
	c := NewClock(epoch())
	c.AfterFunc(time.Second, func() {})
	if err := c.BlockUntilTimers(canceled(), 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("BlockUntilTimers() = %v, want %v", err, context.Canceled)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- c.BlockUntilTimers(ctx, 2) }()
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("BlockUntilTimers() = %v, want %v", err, context.Canceled)
	}
}

func TestBlockUntilTimersCountsOnlyArmedTimers(t *testing.T) {
	c := NewClock(epoch())
	stop := c.AfterFunc(time.Second, func() {})
	c.AfterFunc(time.Second, func() {})
	stop()
	if err := c.BlockUntilTimers(canceled(), 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("BlockUntilTimers() with a stopped timer = %v, want %v", err, context.Canceled)
	}
}

func TestZeroClockIsUsable(t *testing.T) {
	var c Clock
	f, _, done := fired()
	c.AfterFunc(time.Second, f)
	if err := c.BlockUntilTimers(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Second)
	waitFired(t, done)
}

// epoch is the time every test clock starts at.
func epoch() time.Time { return time.Unix(0, 0) }

// canceled returns a context that has already ended.
func canceled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

package daemontest

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"go.vipengele.org/daemon"
)

var _ daemon.Clock = (*Clock)(nil)

// Clock is a deterministic [daemon.Clock]. Its time starts where [NewClock] puts it and moves only
// by [Clock.Advance]; a timer armed by [Clock.AfterFunc] fires only inside a call to Advance. A
// Clock is safe for concurrent use.
//
// The zero Clock reads the zero time; NewClock is the usual way to make one.
type Clock struct {
	// mu guards every field below.
	mu  sync.Mutex
	now time.Time
	// timers are the armed timers: neither fired nor stopped.
	timers []*timer
	// seq numbers timers in the order they were armed.
	seq uint64
	// changed is closed and replaced whenever the set of armed timers changes; nil until first
	// needed.
	changed chan struct{}
}

// timer is one AfterFunc call on a Clock.
type timer struct {
	due time.Time
	seq uint64
	f   func()
}

// NewClock returns a Clock that reads start until it is advanced.
func NewClock(start time.Time) *Clock {
	return &Clock{now: start}
}

// Now returns the clock's current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc arms a timer due at Now plus d and returns the function that stops it. The timer fires
// during the first [Clock.Advance] that reaches its deadline, by running f in its own goroutine. A
// d of zero or less is due at once, yet f still waits for the next Advance, Advance(0) included,
// and never runs inside AfterFunc.
//
// stop disarms the timer and reports whether that prevented f from running; it returns false
// once the timer has fired or been stopped.
func (c *Clock) AfterFunc(d time.Duration, f func()) (stop func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	t := &timer{due: c.now.Add(d), seq: c.seq, f: f}
	c.timers = append(c.timers, t)
	c.signal()
	return func() bool { return c.stop(t) }
}

// stop removes t from the armed timers and reports whether it was still armed.
func (c *Clock) stop(t *timer) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := slices.Index(c.timers, t)
	if i < 0 {
		return false
	}
	c.timers = slices.Delete(c.timers, i, i+1)
	c.signal()
	return true
}

// Advance moves the clock forward by d and fires every timer whose deadline it reaches. The timers
// are taken in deadline order, those with the same deadline in the order they were armed, and each
// callback is started in its own goroutine in that order. Advance does not wait for the callbacks
// to return, so the order in which they run is the scheduler's: a test that needs one to finish
// before the next starts advances to each deadline in turn. A timer is disarmed before its
// callback starts, so its stop reports false from then on.
//
// A negative d panics: the clock never moves backwards.
func (c *Clock) Advance(d time.Duration) {
	if d < 0 {
		panic("daemontest: Clock.Advance with a negative duration")
	}
	for _, f := range c.advance(d) {
		go f()
	}
}

// advance moves the clock forward by d, disarms every timer due at the new time, and returns
// their callbacks in firing order.
func (c *Clock) advance(d time.Duration) []func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)

	var due []*timer
	c.timers = slices.DeleteFunc(c.timers, func(t *timer) bool {
		if t.due.After(c.now) {
			return false
		}
		due = append(due, t)
		return true
	})
	if len(due) == 0 {
		return nil
	}
	c.signal()

	slices.SortFunc(due, func(a, b *timer) int {
		if n := a.due.Compare(b.due); n != 0 {
			return n
		}
		return cmp.Compare(a.seq, b.seq)
	})
	fs := make([]func(), len(due))
	for i, t := range due {
		fs[i] = t.f
	}
	return fs
}

// BlockUntilTimers waits until at least n timers are armed, that is, armed by AfterFunc and
// neither fired nor stopped, and returns nil. It returns at once when that already holds, and
// returns ctx.Err() when ctx ends first. It wakes whenever a timer is armed, fires or is stopped,
// and never polls.
func (c *Clock) BlockUntilTimers(ctx context.Context, n int) error {
	for {
		c.mu.Lock()
		if len(c.timers) >= n {
			c.mu.Unlock()
			return nil
		}
		if c.changed == nil {
			c.changed = make(chan struct{})
		}
		changed := c.changed
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// signal wakes every BlockUntilTimers waiting on the current set of armed timers. The caller
// holds mu.
func (c *Clock) signal() {
	if c.changed != nil {
		close(c.changed)
		c.changed = nil
	}
}

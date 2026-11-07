package daemon

import (
	"context"
	"sync"
	"time"
)

// Clock is the time a Supervisor measures its deadlines against: the start timeout, the grace
// period and the deadline of a reload round. WithClock sets it; without that option it is the
// system clock. A test passes a Clock it advances itself, so that a deadline expires without
// waiting for it.
type Clock interface {
	// Now returns the current time on the clock.
	Now() time.Time

	// AfterFunc runs f in its own goroutine once d has elapsed on the clock. The returned stop
	// cancels that call and reports whether it prevented it, as time.Timer.Stop does.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// realClock is the system clock.
type realClock struct{}

// Now returns time.Now().
func (realClock) Now() time.Time { return time.Now() }

// AfterFunc runs f after d through time.AfterFunc.
func (realClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// withDeadline returns a context that carries the values and the cancellation of parent and
// expires once d has elapsed on clock, and the function that releases it. With the system clock
// it is context.WithTimeout. With any other clock the deadline is clock.Now().Add(d), or the
// parent's when that is earlier, and the context reports context.DeadlineExceeded once the
// clock's timer fires. A d of zero or less returns a context that has already expired.
func withDeadline(clock Clock, parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := clock.(realClock); ok {
		return context.WithTimeout(parent, d)
	}

	c := &clockContext{parent: parent, deadline: clock.Now().Add(d), done: make(chan struct{})}
	if pd, ok := parent.Deadline(); ok && pd.Before(c.deadline) {
		c.deadline = pd
	}
	if err := parent.Err(); err != nil {
		c.end(err)
		return c, func() {}
	}
	if d <= 0 {
		c.end(context.DeadlineExceeded)
		return c, func() {}
	}

	// Either watcher may fire before both are recorded; end then finds the other unrecorded, and
	// the check below stops it once it is.
	stopParent := context.AfterFunc(parent, func() { c.end(parent.Err()) })
	stopTimer := clock.AfterFunc(d, func() { c.end(context.DeadlineExceeded) })
	c.mu.Lock()
	c.stopParent, c.stopTimer = stopParent, stopTimer
	ended := c.err != nil
	c.mu.Unlock()
	if ended {
		c.release()
	}
	return c, func() { c.end(context.Canceled) }
}

// clockContext is the context withDeadline returns for a clock other than the system clock. The
// first of the timer, the end of the parent and the cancel function sets err and closes done;
// the ones after it change nothing. Ending it stops both watchers, so neither outlives it.
type clockContext struct {
	parent   context.Context
	deadline time.Time
	done     chan struct{}

	// mu guards err and the stop functions of the two watchers, which are nil once called.
	mu         sync.Mutex
	err        error
	stopParent func() bool
	stopTimer  func() bool
}

// Deadline returns the clock's deadline, or the parent's when that is earlier.
func (c *clockContext) Deadline() (time.Time, bool) { return c.deadline, true }

// Done returns a channel that closes once the context has ended.
func (c *clockContext) Done() <-chan struct{} { return c.done }

// Err returns nil while the context is live, and then why it ended: context.DeadlineExceeded when
// the clock's timer fired, the parent's error when the parent ended, and context.Canceled when the
// cancel function was called, whichever came first.
func (c *clockContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Value returns the parent's value for key.
func (c *clockContext) Value(key any) any { return c.parent.Value(key) }

// end ends the context with err unless it has already ended, and stops both watchers.
func (c *clockContext) end(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
	c.mu.Unlock()
	c.release()
}

// release stops the watchers that have been recorded and not yet stopped.
func (c *clockContext) release() {
	c.mu.Lock()
	stopParent, stopTimer := c.stopParent, c.stopTimer
	c.stopParent, c.stopTimer = nil, nil
	c.mu.Unlock()
	if stopParent != nil {
		stopParent()
	}
	if stopTimer != nil {
		stopTimer()
	}
}

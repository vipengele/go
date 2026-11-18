package clocktest

import (
	"context"
	"sync"
	"time"
)

// Manual is a clock.Clock whose time moves only through Advance. Its methods are safe to call from
// any goroutine.
//
// Manual departs from the clock.Clock contract in one respect: AfterFunc does not run f in its own
// goroutine, but synchronously inside the Advance call that reaches its deadline, in deadline
// order with the sleeps that call wakes. A test therefore observes every effect of a deferred
// call as soon as Advance returns.
type Manual struct {
	// advancing serialises Advance, so two concurrent calls fire their waits in a single order.
	advancing sync.Mutex

	mu      sync.Mutex
	now     time.Time
	waiting []*wait
}

// wait is a pending timer or sleep. Exactly one of f and wake is set.
type wait struct {
	at   time.Time
	f    func()
	wake chan struct{}
}

// New returns a Manual clock reading start.
func New(start time.Time) *Manual {
	return &Manual{now: start}
}

// Now returns the time on the clock. While Advance runs a timer's f, it reads that timer's
// deadline.
func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// Sleep blocks until an Advance reaches now+d or ctx ends. It returns ctx.Err() when ctx ends
// first, including when ctx has already ended, and nil when Advance wakes it. When both happen
// together it returns ctx.Err(). A d of zero or less returns at once.
func (m *Manual) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}

	w := &wait{wake: make(chan struct{})}
	m.add(w, d)
	select {
	case <-w.wake:
		return nil
	case <-ctx.Done():
		m.remove(w)
		return ctx.Err()
	}
}

// AfterFunc arranges for f to run when an Advance reaches now+d. f runs synchronously inside that
// Advance, in deadline order, and must not call Advance, which would deadlock. A d of zero or less
// makes f due at the current time, so it runs on the next Advance, Advance(0) included, and never
// inside AfterFunc itself.
//
// The returned stop removes f and reports whether it did so: false once f has started or stop has
// already been called.
func (m *Manual) AfterFunc(d time.Duration, f func()) func() bool {
	w := &wait{f: f}
	m.add(w, max(d, 0))
	return func() bool { return m.remove(w) }
}

// Advance moves the clock forward by d. Every timer and sleep falling due by the new time fires,
// in deadline order and, for equal deadlines, in the order they were registered; the clock reads
// each one's deadline as it fires and now+d once Advance returns. A timer or sleep that a firing f
// registers within the window fires in the same call. Advance panics when d is negative, since the
// clock never moves backwards.
func (m *Manual) Advance(d time.Duration) {
	if d < 0 {
		panic("clocktest: Advance with a negative duration")
	}
	m.advancing.Lock()
	defer m.advancing.Unlock()

	m.mu.Lock()
	end := m.now.Add(d)
	m.mu.Unlock()

	for {
		w := m.takeDue(end)
		if w == nil {
			return
		}
		// mu is not held here, so f may read the clock, register waits and stop timers.
		if w.f != nil {
			w.f()
		} else {
			close(w.wake)
		}
	}
}

// Pending returns the number of timers and sleeps waiting on the clock. A timer that has fired or
// been stopped, and a sleep that has been woken or whose context has ended, are not counted.
func (m *Manual) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.waiting)
}

// add registers w as due d after the current time.
func (m *Manual) add(w *wait, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.at = m.now.Add(d)
	m.waiting = append(m.waiting, w)
}

// remove takes w off the clock and reports whether it was still waiting.
func (m *Manual) remove(w *wait) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, x := range m.waiting {
		if x == w {
			m.waiting = append(m.waiting[:i], m.waiting[i+1:]...)
			return true
		}
	}
	return false
}

// takeDue removes and returns the first wait due by end, setting the clock to its deadline: the
// earliest deadline, and among equal deadlines the earliest registered, since waiting stays in
// registration order. When none is due it sets the clock to end and returns nil.
func (m *Manual) takeDue(end time.Time) *wait {
	m.mu.Lock()
	defer m.mu.Unlock()

	first := -1
	for i, w := range m.waiting {
		if w.at.After(end) {
			continue
		}
		if first < 0 || w.at.Before(m.waiting[first].at) {
			first = i
		}
	}
	if first < 0 {
		if end.After(m.now) {
			m.now = end
		}
		return nil
	}

	w := m.waiting[first]
	m.waiting = append(m.waiting[:first], m.waiting[first+1:]...)
	if w.at.After(m.now) {
		m.now = w.at
	}
	return w
}

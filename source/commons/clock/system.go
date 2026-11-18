package clock

import (
	"context"
	"time"
)

// System returns the clock of the operating system. Every call returns an equal value, so
// IsSystem recognises it wherever it came from.
func System() Clock { return systemClock{} }

// IsSystem reports whether c is the clock System returns. Code that holds a Clock uses it to take
// a path the standard library already provides, such as context.WithTimeout, only when the clock
// is the real one.
func IsSystem(c Clock) bool {
	_, ok := c.(systemClock)
	return ok
}

// systemClock is the clock of the operating system. It has no fields, so System needs no
// package-level variable to hand out a single instance.
type systemClock struct{}

// Now returns time.Now().
func (systemClock) Now() time.Time { return time.Now() }

// Sleep waits on a timer it stops before returning, so a Sleep that ctx ends early leaves no
// timer behind. The timer of a d of zero or less is already due, so it returns at once.
func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// AfterFunc runs f after d through time.AfterFunc.
func (systemClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

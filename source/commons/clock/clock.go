package clock

import (
	"context"
	"time"
)

// Clock is a source of the current time and of waits measured against it. System returns the
// clock of the operating system; a test passes an implementation it advances itself, so that a
// wait ends without the time passing.
type Clock interface {
	// Now returns the current time on the clock.
	Now() time.Time

	// Sleep blocks until d has elapsed on the clock or ctx ends, whichever comes first. It returns
	// ctx.Err() when ctx ends first, including when ctx has already ended, and nil otherwise. A d
	// of zero or less returns at once.
	Sleep(ctx context.Context, d time.Duration) error

	// AfterFunc runs f in its own goroutine once d has elapsed on the clock. The returned stop
	// cancels that call and reports whether it prevented it, as time.Timer.Stop does: false once f
	// has started or stop has already been called.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

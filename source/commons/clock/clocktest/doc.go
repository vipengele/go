// Package clocktest provides a clock.Clock that a test moves itself, so that code which sleeps or
// defers a call through a clock runs without real time passing.
//
// The clock New returns stands still until the test calls Advance. A test that drives code
// sleeping in another goroutine waits for that sleep to register before advancing, by polling
// Pending:
//
//	c := clocktest.New(time.Unix(0, 0))
//	go poll(ctx, c) // sleeps a second between rounds
//	for c.Pending() < 1 {
//		runtime.Gosched()
//	}
//	c.Advance(time.Second) // wakes the sleep, and poll runs its next round
package clocktest

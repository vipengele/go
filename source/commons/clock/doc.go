// Package clock is the source of time for code that waits or schedules a call, so that a test can
// substitute a clock it advances itself.
//
// Code takes a Clock and reads, sleeps and defers calls through it. System returns the clock of
// the operating system, which production code passes:
//
//	func poll(ctx context.Context, c clock.Clock) error {
//		for {
//			if err := c.Sleep(ctx, time.Second); err != nil {
//				return err
//			}
//			// ...
//		}
//	}
//
//	err := poll(ctx, clock.System())
package clock

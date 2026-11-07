// Package daemontest is a kit for tests of code built on [go.vipengele.org/daemon], written
// against that package's exported API only (ADR-0028).
//
// [Clock] is a fake [daemon.Clock]: its time moves only when the test calls [Clock.Advance], so a
// start timeout, a stop grace, a reload round or a health check timeout expires without waiting
// for it. [Clock.BlockUntilTimers] waits until the code under test has armed its timers, so the
// test advances the clock after a deadline exists rather than before. [NewRunner] returns a
// scripted [daemon.Runner] whose Start can fail, hang until its context ends, or succeed, whose
// Stop can return an error or ignore its context, and which records the calls it receives.
// [Start] runs a [daemon.Supervisor] serving on 127.0.0.1:0, returns once every Runner has
// started or once Run has returned, and stops it when the test ends. A Runner whose Start hangs
// keeps Start from returning until the start timeout expires, so a test of that timeout drives
// the Supervisor itself.
//
// A test of code served over HTTP starts a daemon with a handler:
//
//	d := daemontest.Start(t, daemontest.Config{Handler: h})
//	resp, err := http.Get(d.URL() + "/")
//	if err != nil {
//		t.Fatal(err)
//	}
//	defer resp.Body.Close()
//	d.Stop()
//
// A test of a start timeout hangs a Runner's Start and advances the clock past the timeout:
//
//	clock := daemontest.NewClock(time.Unix(0, 0))
//	r := daemontest.NewRunner(t, "slow", daemontest.HangStart())
//	s, err := daemon.New([]daemon.Runner{r},
//		daemon.WithClock(clock), daemon.WithStartTimeout(time.Minute))
//	if err != nil {
//		t.Fatal(err)
//	}
//	done := make(chan error, 1)
//	go func() { done <- s.Run(ctx) }()
//	if err := clock.BlockUntilTimers(ctx, 1); err != nil {
//		t.Fatal(err)
//	}
//	clock.Advance(time.Minute)
//	if err := <-done; !errors.Is(err, daemon.ErrStartTimeout) {
//		t.Fatalf("Run = %v, want ErrStartTimeout", err)
//	}
package daemontest

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package daemon

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"
)

// signalGuard bounds each wait of the real-signal test so that a lost signal fails it rather than
// hangs it.
const signalGuard = 30 * time.Second

func TestARealSIGHUPRunsOneReloadRound(t *testing.T) {
	log := &reloadLog[int]{}
	runner := newReloading("a", log)
	runner.rec = &recorder{}
	s, err := New([]Runner{runner},
		WithReload(func(context.Context) (int, error) { return 7, nil }),
		WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{}, 8)
	s.waiting = func() { waiting <- struct{}{} }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.Run(ctx) }()

	await := func(what string, c <-chan struct{}) {
		t.Helper()
		select {
		case <-c:
		case err := <-result:
			t.Fatalf("Run returned %v before %s", err, what)
		case <-time.After(signalGuard):
			t.Fatalf("timed out waiting for %s", what)
		}
	}

	// Run has subscribed to SIGHUP before it starts waiting, so the signal is not lost.
	await("Run to wait", waiting)
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.reloading:
	case err := <-result:
		t.Fatalf("Run returned %v before the Reload", err)
	case <-time.After(signalGuard):
		t.Fatal("timed out waiting for the Reload")
	}
	await("Run to wait after the round", waiting)

	calls := log.all()
	if len(calls) != 1 || calls[0].runner != "a" || calls[0].cfg != 7 {
		t.Errorf("Reload calls = %+v, want one of 7 on a", calls)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(signalGuard):
		t.Fatal("timed out waiting for Run to return")
	}
}

package daemon

import (
	"errors"
	"sync/atomic"
	"testing"
)

// countReady returns a WithReady option that counts its calls in n.
func countReady(n *atomic.Int32) Option {
	return WithReady(func() { n.Add(1) })
}

func checkReadyCalls(t *testing.T, n *atomic.Int32, want int32) {
	t.Helper()
	if got := n.Load(); got != want {
		t.Errorf("ready called %d times, want %d", got, want)
	}
}

func TestNewRejectsANilReady(t *testing.T) {
	s, err := New([]Runner{&fakeRunner{name: "a"}}, WithReady(nil))
	if err == nil {
		t.Fatal("New with a nil ready succeeded")
	}
	if s != nil {
		t.Error("New returned a Supervisor alongside an error")
	}
}

func TestReadyFiresOnceAfterEveryStartHasReturned(t *testing.T) {
	var n atomic.Int32
	a, b := newScripted("a"), newScripted("b")
	b.hangStart = true
	h := launchRun(t, []Option{countReady(&n)}, a, b)

	<-a.started
	<-b.started
	checkReadyCalls(t, &n, 0)

	b.free()
	<-h.waiting
	checkReadyCalls(t, &n, 1)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkReadyCalls(t, &n, 1)
	h.finish()
}

func TestReadyDoesNotFireWhenTheStartPhaseFails(t *testing.T) {
	boom := errors.New("boom")
	var n atomic.Int32
	a, b := newScripted("a"), newScripted("b")
	b.startErr = boom
	h := launchRun(t, []Option{countReady(&n)}, a, b)

	if err := <-h.result; !errors.Is(err, boom) {
		t.Errorf("Run = %v, want the start error %v", err, boom)
	}
	checkReadyCalls(t, &n, 0)
	h.finish()
}

func TestReadyDoesNotFireAgainAfterAReloadRound(t *testing.T) {
	var n, loads atomic.Int32
	log := &reloadLog[int]{}
	a := newReloading("a", log)
	h, sub := runReloading(t, counted(&loads, 3), []Option{countReady(&n)}, a)
	checkReadyCalls(t, &n, 1)

	for range 2 {
		hangup(sub)
		<-h.waiting
	}
	checkReloadCalls(t, log.all(), []reloadCall[int]{{"a", 3}, {"a", 3}})
	checkReadyCalls(t, &n, 1)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	h.finish()
}

func TestReadyDoesNotFireAgainAfterAnIsolatedPanicExit(t *testing.T) {
	var n atomic.Int32
	a, b := newScripted("a"), newScripted("b")
	h := runScriptedWith(t, []Option{WithPanicPolicy(PanicIsolate), countReady(&n)}, a, b)
	checkReadyCalls(t, &n, 1)

	a.panicExit(errors.New("a"))
	<-h.waiting
	checkReadyCalls(t, &n, 1)

	h.cancelRun()
	checkJoined(t, <-h.result, []wantErr{{panicked: "a"}})
	checkReadyCalls(t, &n, 1)
	h.finish()
}

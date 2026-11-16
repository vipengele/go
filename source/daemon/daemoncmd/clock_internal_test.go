package daemoncmd

import (
	"testing"
	"time"
)

func TestSystemClockNowIsTheTimeOfDay(t *testing.T) {
	before := time.Now()
	got := systemClock{}.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("Now = %v, want one between %v and %v", got, before, after)
	}
}

func TestSystemClockAfterFuncRunsFAfterD(t *testing.T) {
	fired := make(chan struct{})
	systemClock{}.AfterFunc(time.Millisecond, func() { close(fired) })
	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("f did not run within 10s")
	}
}

func TestSystemClockAfterFuncStopsFBeforeItRuns(t *testing.T) {
	ran := make(chan struct{})
	stop := systemClock{}.AfterFunc(time.Hour, func() { close(ran) })
	if !stop() {
		t.Error("stop = false, want true for a timer that has not fired")
	}
	if stop() {
		t.Error("stop = true on its second call, want false")
	}
	select {
	case <-ran:
		t.Error("f ran after stop")
	default:
	}
}

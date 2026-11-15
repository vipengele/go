package selfupdate

import (
	"testing"
	"time"
)

func TestSystemClockNowIsTheCurrentTime(t *testing.T) {
	t.Parallel()

	before := time.Now()
	got := systemClock{}.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("Now() = %v, want within [%v, %v]", got, before, after)
	}
}

func TestSystemClockAfterFuncFiresAfterTheDelay(t *testing.T) {
	t.Parallel()

	fired := make(chan struct{})
	systemClock{}.AfterFunc(time.Millisecond, func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(30 * time.Second):
		t.Fatal("AfterFunc never ran f")
	}
}

func TestSystemClockAfterFuncStopPreventsTheCall(t *testing.T) {
	t.Parallel()

	stop := systemClock{}.AfterFunc(time.Hour, func() { t.Error("f ran after stop") })

	if !stop() {
		t.Error("stop() = false, want true for a timer that had not fired")
	}

	if stop() {
		t.Error("second stop() = true, want false")
	}
}

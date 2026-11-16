package daemon

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"go.vipengele.org/daemon/systemd"
)

// eventLog is the ordered record of what a test observes across the update guard, the Runners and
// the ready callback, written from Run's goroutine and from each Start's.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (e *eventLog) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *eventLog) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.events)
}

// fakeGuard is an UpdateGuard driven by its test. Boot returns rolledBack and bootErr, Confirm
// returns confirmErr; each records itself in events when that is set. With holdConfirm, Confirm
// reports itself on confirmEntered and blocks until the test closes confirmRelease.
type fakeGuard struct {
	events     *eventLog
	rolledBack bool
	bootErr    error
	confirmErr error

	holdConfirm    bool
	confirmEntered chan struct{}
	confirmRelease chan struct{}

	boots    atomic.Int32
	confirms atomic.Int32
}

func newFakeGuard(events *eventLog) *fakeGuard {
	return &fakeGuard{
		events:         events,
		confirmEntered: make(chan struct{}, 1),
		confirmRelease: make(chan struct{}),
	}
}

func (g *fakeGuard) Boot(context.Context) (bool, error) {
	g.boots.Add(1)
	if g.events != nil {
		g.events.add("boot")
	}
	return g.rolledBack, g.bootErr
}

func (g *fakeGuard) Confirm(context.Context) error {
	g.confirms.Add(1)
	if g.events != nil {
		g.events.add("confirm")
	}
	if g.holdConfirm {
		g.confirmEntered <- struct{}{}
		<-g.confirmRelease
	}
	return g.confirmErr
}

// checkCount fails the test unless n holds want calls of what.
func checkCount(t *testing.T, what string, n *atomic.Int32, want int32) {
	t.Helper()
	if got := n.Load(); got != want {
		t.Errorf("%s called %d times, want %d", what, got, want)
	}
}

// checkNothingStarted fails the test when any Start of h's Runners was called.
func checkNothingStarted(t *testing.T, h *harness) {
	t.Helper()
	if got := h.rec.startOrder(); len(got) != 0 {
		t.Errorf("Start called on %v, want no Runner started", got)
	}
}

// guardRecords returns the records in log about the update guard.
func guardRecords(log *capturedLog) []logged {
	var out []logged
	for _, r := range log.all() {
		if r.msg == msgUpdateRollbackUnrecorded || r.msg == msgUpdateConfirmFailed {
			out = append(out, r)
		}
	}
	return out
}

// checkGuardRecord fails the test unless records is one record at level with msg, no runner and
// an err attribute wrapping want.
func checkGuardRecord(t *testing.T, records []logged, level slog.Level, msg string, want error) {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("update guard records = %v, want one %q", records, msg)
	}
	r := records[0]
	if r.level != level || r.msg != msg {
		t.Errorf("record = %v %q, want %v %q", r.level, r.msg, level, msg)
	}
	if v, ok := r.attrs["runner"]; ok {
		t.Errorf("record %q: runner = %v, want none", r.msg, v)
	}
	v, ok := r.attrs["err"]
	if !ok {
		t.Fatalf("record %q: no err attribute", r.msg)
	}
	if err, isErr := v.Any().(error); !isErr || !errors.Is(err, want) {
		t.Errorf("record %q: err = %v, want it to wrap %v", r.msg, v.Any(), want)
	}
}

func TestNewRejectsANilUpdateGuard(t *testing.T) {
	s, err := New([]Runner{&fakeRunner{name: "a"}}, WithUpdateGuard(nil))
	if err == nil {
		t.Fatal("New with a nil update guard succeeded")
	}
	if s != nil {
		t.Error("New returned a Supervisor alongside an error")
	}
}

func TestUpdateGuardBootsBeforeAnyStartAndConfirmsOnceAfterReady(t *testing.T) {
	events := &eventLog{}
	g := newFakeGuard(events)
	a, b := newScripted("a"), newScripted("b")
	a.onStart = func() { events.add("start a") }
	b.onStart = func() { events.add("start b") }
	ready := WithReady(func() { events.add("ready") })
	h := runScriptedWith(t, []Option{WithUpdateGuard(g), ready}, a, b)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkOrder(t, "events", events.all(), []string{"boot", "start a", "start b", "ready", "confirm"})
	checkCount(t, "Boot", &g.boots, 1)
	checkCount(t, "Confirm", &g.confirms, 1)
	h.finish()
}

func TestUpdateGuardBootErrorIsReturnedAndStartsNothing(t *testing.T) {
	boom := errors.New("boom")
	g := newFakeGuard(nil)
	g.bootErr = boom
	h := launchRun(t, []Option{WithUpdateGuard(g)}, newScripted("a"))

	err := <-h.result
	if !errors.Is(err, boom) {
		t.Errorf("Run = %v, want the Boot error %v", err, boom)
	}
	if RestartRequested(err) {
		t.Errorf("RestartRequested(%v) = true, want false", err)
	}
	checkNothingStarted(t, h)
	checkCount(t, "Confirm", &g.confirms, 0)
	h.finish()
}

func TestUpdateGuardRollbackRequestsARestartAndStartsNothing(t *testing.T) {
	storeErr := errors.New("store failed")
	tests := []struct {
		name    string
		bootErr error
	}{
		{name: "recorded"},
		{name: "record not stored", bootErr: storeErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, logger := newCapture()
			g := newFakeGuard(nil)
			g.rolledBack, g.bootErr = true, tt.bootErr
			h := launchRun(t, []Option{WithUpdateGuard(g), WithLogger(logger)}, newScripted("a"))

			err := <-h.result
			if !RestartRequested(err) {
				t.Errorf("RestartRequested(%v) = false, want true", err)
			}
			if errors.Is(err, storeErr) {
				t.Errorf("Run = %v, want the store error left out", err)
			}
			checkNothingStarted(t, h)
			checkCount(t, "Confirm", &g.confirms, 0)
			if tt.bootErr == nil {
				if records := guardRecords(logs); len(records) != 0 {
					t.Errorf("update guard records = %v, want none", records)
				}
			} else {
				checkGuardRecord(t, guardRecords(logs), slog.LevelWarn, msgUpdateRollbackUnrecorded, storeErr)
			}
			h.finish()
		})
	}
}

func TestUpdateGuardConfirmIsNeverCalledWhenTheStartPhaseFails(t *testing.T) {
	boom := errors.New("boom")
	g := newFakeGuard(nil)
	a, b := newScripted("a"), newScripted("b")
	b.startErr = boom
	h := launchRun(t, []Option{WithUpdateGuard(g)}, a, b)

	if err := <-h.result; !errors.Is(err, boom) {
		t.Errorf("Run = %v, want the start error %v", err, boom)
	}
	checkCount(t, "Boot", &g.boots, 1)
	checkCount(t, "Confirm", &g.confirms, 0)
	h.finish()
}

func TestUpdateGuardConfirmErrorIsLoggedAndTheRunGoesOn(t *testing.T) {
	boom := errors.New("boom")
	logs, logger := newCapture()
	g := newFakeGuard(nil)
	g.confirmErr = boom
	a := newScripted("a")
	h := runScriptedWith(t, []Option{WithUpdateGuard(g), WithLogger(logger)}, a)

	checkGuardRecord(t, guardRecords(logs), slog.LevelError, msgUpdateConfirmFailed, boom)
	select {
	case <-a.stopCalled:
		t.Error("Stop called after a Confirm error, want the run to go on")
	default:
	}

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkCount(t, "Confirm", &g.confirms, 1)
	h.finish()
}

func TestUpdateGuardConfirmRunsAfterSystemdReady(t *testing.T) {
	path, conn := sdSocket(t)
	g := newFakeGuard(nil)
	g.holdConfirm = true
	h := launchRun(t, []Option{WithUpdateGuard(g), WithSystemd(sdEnv{"NOTIFY_SOCKET": path}.lookup)}, newScripted("a"))

	<-g.confirmEntered
	sdReceive(t, conn, systemd.Ready)
	close(g.confirmRelease)
	<-h.waiting

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	sdReceive(t, conn, systemd.Stopping)
	h.finish()
}

func TestUpdateGuardConfirmRunsAfterTheWindowsServiceReportsRunning(t *testing.T) {
	g := newFakeGuard(nil)
	g.holdConfirm = true
	r := launchSCM(t, []Option{WithUpdateGuard(g)}, newScripted("a"))

	r.scm.expect(t, statusStartPending())
	<-g.confirmEntered
	r.scm.expect(t, statusRunning())
	close(g.confirmRelease)

	r.scm.send(scmStop)
	r.scm.expect(t, statusStopPending(defaultGrace))
	if code := r.scm.exitCode(t); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if err := r.wait(t); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkCount(t, "Confirm", &g.confirms, 1)
	r.finish(t)
}

func TestUpdateGuardRollbackUnderTheWindowsServiceExitsWithTheRestartCode(t *testing.T) {
	g := newFakeGuard(nil)
	g.rolledBack = true
	a := newScripted("a")
	r := launchSCM(t, []Option{WithUpdateGuard(g)}, a)

	r.scm.expect(t, statusStartPending())
	if code := r.scm.exitCode(t); code != WindowsRestartExitCode {
		t.Errorf("exit code = %d, want %d", code, WindowsRestartExitCode)
	}
	if err := r.wait(t); !RestartRequested(err) {
		t.Errorf("Run = %v, want a requested restart", err)
	}
	select {
	case <-a.started:
		t.Error("Start called after a rollback, want no Runner started")
	default:
	}
	checkCount(t, "Confirm", &g.confirms, 0)
	r.scm.quiet(t)
	r.finish(t)
}

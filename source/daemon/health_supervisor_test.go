package daemon

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// checkingRunner is a scripted Runner that is a HealthChecker. CheckHealth counts its calls and
// returns err. With holdErr, Err reports its first call on errCalled and blocks until the test
// closes errRelease, which holds Run's goroutine between the Runner's exit and Run dealing with it.
type checkingRunner struct {
	*scriptedRunner
	calls atomic.Int32
	err   error

	holdErr    bool
	errCalled  chan struct{}
	errRelease chan struct{}
}

func newChecking(name string) *checkingRunner {
	return &checkingRunner{
		scriptedRunner: newScripted(name),
		errCalled:      make(chan struct{}, 1),
		errRelease:     make(chan struct{}),
	}
}

func (r *checkingRunner) CheckHealth(context.Context) error {
	r.calls.Add(1)
	return r.err
}

func (r *checkingRunner) Err() error {
	if r.holdErr {
		select {
		case r.errCalled <- struct{}{}:
		default:
		}
		<-r.errRelease
	}
	return r.scriptedRunner.Err()
}

// launchHealthRun starts Run through launchRun, with h given to New through WithHealth, and
// returns at once.
func launchHealthRun(t *testing.T, h *Health, opts []Option, list ...Runner) *harness {
	t.Helper()
	return launchRun(t, append(slices.Clone(opts), WithHealth(h)), list...)
}

// wantHealth is the expected state of a snapshot: its overall status, and the error text each
// named check fails with, or "" for a check that passes.
type wantHealth struct {
	status State
	checks map[string]string
}

// checkHealth takes a snapshot of h and compares it with want. Every check of the snapshot must be
// named in want.checks, and a failing check's error must contain the text want gives for it.
func checkHealth(t *testing.T, phase string, h *Health, want wantHealth) {
	t.Helper()
	snap := h.Snapshot(context.Background())
	if snap.Status != want.status {
		t.Errorf("%s: status = %q, want %q (checks %+v)", phase, snap.Status, want.status, snap.Checks)
	}
	if snap.Live != (want.status != StateDown) {
		t.Errorf("%s: live = %v with status %q", phase, snap.Live, want.status)
	}
	if snap.Ready != (want.status == StateOK) {
		t.Errorf("%s: ready = %v with status %q", phase, snap.Ready, want.status)
	}
	if len(snap.Checks) != len(want.checks) {
		t.Errorf("%s: %d checks, want %d: %+v", phase, len(snap.Checks), len(want.checks), snap.Checks)
	}
	for _, c := range snap.Checks {
		msg, ok := want.checks[c.Name]
		switch {
		case !ok:
			t.Errorf("%s: unexpected check %q", phase, c.Name)
		case msg == "" && c.Status != CheckPass:
			t.Errorf("%s: check %q = %s %q, want pass", phase, c.Name, c.Status, c.Error)
		case msg != "" && (c.Status != CheckFail || !strings.Contains(c.Error, msg)):
			t.Errorf("%s: check %q = %s %q, want fail with %q", phase, c.Name, c.Status, c.Error, msg)
		}
	}
}

func checkCalls(t *testing.T, phase string, r *checkingRunner, want int32) {
	t.Helper()
	if got := r.calls.Load(); got != want {
		t.Errorf("%s: CheckHealth of %q called %d times, want %d", phase, r.name, got, want)
	}
}

func TestHealthFollowsTheRunFromStartToReturn(t *testing.T) {
	h := NewHealth()
	a, b, c := newScripted("a"), newChecking("b"), newChecking("c")
	b.hangStart = true

	// Stop is called on c first; its snapshot there sees the shutdown in progress.
	stopping := make(chan struct{})
	c.onStop = func() {
		defer close(stopping)
		checkHealth(t, "stopping", h, wantHealth{status: StateUnready, checks: map[string]string{
			"supervisor.live":  "",
			"supervisor.ready": "supervisor is stopping",
			"b":                `runner "b" is not running: supervisor is stopping`,
			"c":                `runner "c" is not running: supervisor is stopping`,
		}})
	}

	hr := launchHealthRun(t, h, nil, a, b, c)
	<-a.started
	<-b.started
	checkHealth(t, "starting", h, wantHealth{status: StateUnready, checks: map[string]string{
		"supervisor.live":  "",
		"supervisor.ready": `supervisor is starting runner "b"`,
		"b":                `runner "b" is not running: starting`,
		"c":                `runner "c" is not running: not started`,
	}})
	checkCalls(t, "starting", b, 0)

	b.free()
	<-c.started
	<-hr.waiting
	checkHealth(t, "running", h, wantHealth{status: StateOK, checks: map[string]string{
		"supervisor.live": "", "supervisor.ready": "", "b": "", "c": "",
	}})
	checkCalls(t, "running", b, 1)
	checkCalls(t, "running", c, 1)

	hr.cancelRun()
	<-stopping
	if err := <-hr.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkCalls(t, "stopping", b, 1)
	checkCalls(t, "stopping", c, 1)

	checkHealth(t, "returned", h, wantHealth{status: StateDown, checks: map[string]string{
		"supervisor.live":  "supervisor has stopped",
		"supervisor.ready": "supervisor has stopped",
		"b":                `runner "b" is not running: supervisor has stopped`,
		"c":                `runner "c" is not running: supervisor has stopped`,
	}})
	checkCalls(t, "returned", b, 1)
	hr.finish()
}

func TestHealthStaysReadyThroughAReloadRoundAndAReloadFailureEndsTheRun(t *testing.T) {
	health := NewHealth()
	log := &reloadLog[int]{}
	a, b := newReloading("a", log), newChecking("b")
	a.hang = true
	cause := errors.New("cannot apply")
	var loads atomic.Int32
	h, sub := runReloading(t, counted(&loads, 1), []Option{WithHealth(health)}, a, b)

	hangup(sub)
	<-a.reloading
	checkHealth(t, "reloading", health, wantHealth{status: StateOK, checks: map[string]string{
		"supervisor.live": "", "supervisor.ready": "", "b": "",
	}})
	checkCalls(t, "reloading", b, 1)
	a.free()
	<-h.waiting
	checkStillRunning(t, h)

	a.hang, a.err = false, cause
	hangup(sub)
	if err := <-h.result; !errors.Is(err, cause) {
		t.Errorf("Run = %v, want %v", err, cause)
	}
	checkHealth(t, "returned", health, wantHealth{status: StateDown, checks: map[string]string{
		"supervisor.live":  "supervisor has stopped",
		"supervisor.ready": "supervisor has stopped",
		"b":                `runner "b" is not running: supervisor has stopped`,
	}})
	h.finish()
}

func TestHealthIsDownBeforeRun(t *testing.T) {
	h := NewHealth()
	b := newChecking("b")
	if _, err := New([]Runner{b}, WithHealth(h)); err != nil {
		t.Fatalf("New: %v", err)
	}
	checkHealth(t, "before Run", h, wantHealth{status: StateDown, checks: map[string]string{
		"supervisor.live":  "supervisor has not started",
		"supervisor.ready": "supervisor has not started",
		"b":                `runner "b" is not running: not started`,
	}})
	checkCalls(t, "before Run", b, 0)
}

func TestHealthIsolatedPanicIsUnreadyButLive(t *testing.T) {
	h := NewHealth()
	a, b := newChecking("a"), newChecking("b")
	hr := launchHealthRun(t, h, []Option{WithPanicPolicy(PanicIsolate)}, a, b)
	<-a.started
	<-b.started
	<-hr.waiting

	b.panicExit("boom")
	<-hr.waiting
	checkHealth(t, "isolated", h, wantHealth{status: StateUnready, checks: map[string]string{
		"supervisor.live":  "",
		"supervisor.ready": `runner "b" exited with a panic and is isolated`,
		"a":                "",
		"b":                `runner "b" is not running: exited with a panic and is isolated`,
	}})
	checkCalls(t, "isolated", a, 1)
	checkCalls(t, "isolated", b, 0)

	hr.cancelRun()
	checkPanic(t, <-hr.result, "b", "boom", "panicExit")
	hr.finish()
}

func TestHealthCheckerIsNotCalledOnceItsRunnerHasExited(t *testing.T) {
	h := NewHealth()
	a, b := newScripted("a"), newChecking("b")
	b.holdErr = true

	// Stop is called on a once shutdown has begun; its snapshot there sees the run stopping.
	stopping := make(chan struct{})
	a.onStop = func() {
		defer close(stopping)
		checkHealth(t, "stopping", h, wantHealth{status: StateUnready, checks: map[string]string{
			"supervisor.live":  "",
			"supervisor.ready": "supervisor is stopping",
			"b":                `runner "b" is not running: supervisor is stopping`,
		}})
	}

	hr := launchHealthRun(t, h, nil, a, b)
	<-a.started
	<-b.started
	<-hr.waiting

	// Run's goroutine is held in b's Err: b's Done has closed and Run has not dealt with the exit.
	b.exit(nil)
	<-b.errCalled
	checkHealth(t, "exited", h, wantHealth{status: StateUnready, checks: map[string]string{
		"supervisor.live":  "",
		"supervisor.ready": `runner "b" exited`,
		"b":                `runner "b" is not running: exited`,
	}})
	checkCalls(t, "exited", b, 0)

	close(b.errRelease)
	<-stopping
	checkCalls(t, "stopping", b, 0)
	if err := <-hr.result; !errors.Is(err, ErrRunnerExited) {
		t.Errorf("Run = %v, want ErrRunnerExited", err)
	}
	checkCalls(t, "returned", b, 0)
	hr.finish()
}

func TestHealthReportsAFailingHealthChecker(t *testing.T) {
	h := NewHealth()
	b := newChecking("b")
	b.err = errors.New("queue unreachable")
	hr := launchHealthRun(t, h, nil, b)
	<-b.started
	<-hr.waiting
	checkHealth(t, "running", h, wantHealth{status: StateUnready, checks: map[string]string{
		"supervisor.live": "", "supervisor.ready": "", "b": "queue unreachable",
	}})
	hr.cancelRun()
	if err := <-hr.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	hr.finish()
}

func TestHealthIsDownOnceAStartFailureEndsTheRun(t *testing.T) {
	h := NewHealth()
	a, b := newChecking("a"), newScripted("b")
	b.startErr = errors.New("bind failed")
	hr := launchHealthRun(t, h, nil, a, b)
	<-a.started
	<-b.started
	if err := <-hr.result; err == nil {
		t.Fatalf("Run = nil, want the start failure")
	}
	checkHealth(t, "returned", h, wantHealth{status: StateDown, checks: map[string]string{
		"supervisor.live":  "supervisor has stopped",
		"supervisor.ready": "supervisor has stopped",
		"a":                "supervisor has stopped",
	}})
	hr.finish()
}

func TestNewRegistersTheSupervisorChecks(t *testing.T) {
	h := NewHealth()
	if _, err := New([]Runner{&fakeRunner{"plain"}, newChecking("b")}, WithHealth(h)); err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := h.Snapshot(context.Background())
	want := map[string][]Kind{
		"b":                {Readiness},
		"supervisor.live":  {Liveness},
		"supervisor.ready": {Readiness},
	}
	if len(snap.Checks) != len(want) {
		t.Fatalf("checks = %+v, want %v", snap.Checks, want)
	}
	for _, c := range snap.Checks {
		if !slices.Equal(c.Kinds, want[c.Name]) {
			t.Errorf("check %q kinds = %v, want %v", c.Name, c.Kinds, want[c.Name])
		}
	}
}

func TestNewWithHealthRejectsANilHealth(t *testing.T) {
	if s, err := New([]Runner{&fakeRunner{"a"}}, WithHealth(nil)); err == nil || s != nil {
		t.Errorf("New(WithHealth(nil)) = %v, %v, want an error", s, err)
	}
}

func TestNewFailingLeavesTheHealthAsItWas(t *testing.T) {
	tests := []struct {
		name    string
		held    []string
		runners []Runner
		opts    []Option
		is      error
	}{
		{
			name:    "runner name taken by a check",
			held:    []string{"b"},
			runners: []Runner{newChecking("a"), newChecking("b")},
			is:      ErrDuplicateCheck,
		},
		{
			name:    "runner named like a supervisor check",
			runners: []Runner{newChecking("supervisor.ready")},
			is:      ErrDuplicateCheck,
		},
		{
			name:    "supervisor check name taken",
			held:    []string{"supervisor.live"},
			runners: []Runner{newChecking("a")},
			is:      ErrDuplicateCheck,
		},
		{
			name:    "invalid option",
			runners: []Runner{newChecking("a")},
			opts:    []Option{WithGrace(0)},
		},
		{
			name:    "duplicate runner names",
			runners: []Runner{newChecking("a"), newChecking("a")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHealth()
			for _, name := range tt.held {
				if err := h.Add(name, func(context.Context) error { return nil }); err != nil {
					t.Fatalf("Add(%q): %v", name, err)
				}
			}
			s, err := New(tt.runners, append(slices.Clone(tt.opts), WithHealth(h))...)
			if err == nil || s != nil {
				t.Fatalf("New = %v, %v, want an error", s, err)
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("New error = %v, want %v", err, tt.is)
			}
			var names []string
			for _, c := range h.Snapshot(context.Background()).Checks {
				names = append(names, c.Name)
			}
			if !slices.Equal(names, tt.held) {
				t.Errorf("checks after a failed New = %v, want %v", names, tt.held)
			}
		})
	}
}

func TestRunnerPhaseString(t *testing.T) {
	for p, want := range map[runnerPhase]string{
		runnerIdle:     "not started",
		runnerStarting: "starting",
		runnerRunning:  "running",
		runnerExited:   "exited",
		runnerIsolated: "exited with a panic and is isolated",
	} {
		if got := p.String(); got != want {
			t.Errorf("runnerPhase(%d).String() = %q, want %q", int(p), got, want)
		}
	}
}

func TestFeedBetweenTwoStartsIsStartingWithNoRunnerStarting(t *testing.T) {
	var f feed
	f.track([]Runner{&fakeRunner{"a"}, &fakeRunner{"b"}})
	f.setPhase(phaseStarting)
	f.setRunner("a", runnerRunning, make(chan struct{}))

	if err := f.ready(); err == nil || err.Error() != "daemon: supervisor is starting" {
		t.Errorf("ready() = %v, want the supervisor starting with no Runner named", err)
	}
	if err := f.running("a"); err != nil {
		t.Errorf("running(a) = %v, want nil", err)
	}
	if err := f.running("b"); err == nil || !strings.Contains(err.Error(), "not started") {
		t.Errorf("running(b) = %v, want b not started", err)
	}
}

func TestFeedTakesARunningRunnerWhoseDoneClosedAsExited(t *testing.T) {
	var f feed
	f.track([]Runner{&fakeRunner{"a"}})
	f.setPhase(phaseRunning)
	done := make(chan struct{})
	f.setRunner("a", runnerRunning, done)
	if err := f.ready(); err != nil {
		t.Fatalf("ready() = %v while a runs, want nil", err)
	}

	close(done)
	if err := f.ready(); err == nil || err.Error() != `daemon: runner "a" exited` {
		t.Errorf("ready() = %v, want a exited", err)
	}
	if err := f.running("a"); err == nil || !strings.HasSuffix(err.Error(), "is not running: exited") {
		t.Errorf("running(a) = %v, want a not running: exited", err)
	}
}

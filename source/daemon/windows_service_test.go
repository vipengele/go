package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"testing"
	"time"
)

// fakeSCM stands in for the Windows service control manager: the test sends controls on requests
// and reads each status the handler reports on statuses. code receives the exit code the handler
// returned, and runErr is what the dispatcher returns once it has.
type fakeSCM struct {
	requests chan scmRequest
	statuses chan scmStatus
	code     chan uint32
	runErr   error
}

func newFakeSCM() *fakeSCM {
	return &fakeSCM{
		requests: make(chan scmRequest),
		statuses: make(chan scmStatus, 16),
		code:     make(chan uint32, 1),
	}
}

// run is the runWindowsService seam: it serves execute as the dispatcher does.
func (f *fakeSCM) run(execute scmExecute) error {
	f.code <- execute(f.requests, func(st scmStatus) { f.statuses <- st })
	return f.runErr
}

// next returns the next status the handler reports, failing the test when none comes in a few
// seconds.
func (f *fakeSCM) next(t *testing.T) scmStatus {
	t.Helper()
	select {
	case st := <-f.statuses:
		if st.State == scmStopped {
			t.Errorf("status %+v reported, want the stop reported only through the exit code", st)
		}
		return st
	case <-time.After(5 * time.Second):
		t.Fatal("no status reported")
		return scmStatus{}
	}
}

// expect fails the test unless the next status reported is want.
func (f *fakeSCM) expect(t *testing.T, want scmStatus) {
	t.Helper()
	if got := f.next(t); got != want {
		t.Errorf("status = %+v, want %+v", got, want)
	}
}

// quiet fails the test when a status is waiting.
func (f *fakeSCM) quiet(t *testing.T) {
	t.Helper()
	select {
	case st := <-f.statuses:
		t.Errorf("status %+v reported, want none", st)
	default:
	}
}

// send delivers the control cmd to the handler.
func (f *fakeSCM) send(cmd scmCmd) {
	f.requests <- scmRequest{Cmd: cmd}
}

// exitCode returns the exit code the handler returned.
func (f *fakeSCM) exitCode(t *testing.T) uint32 {
	t.Helper()
	select {
	case c := <-f.code:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return")
		return 0
	}
}

// statusStartPending, statusRunning and statusStopPending are the statuses the handler reports for a
// Supervisor with the default start timeout and with grace.
func statusStartPending() scmStatus {
	return scmStatus{State: scmStartPending, WaitHint: uint32((30 * time.Second).Milliseconds())}
}

func statusRunning() scmStatus {
	return scmStatus{State: scmRunning, Accepts: scmAcceptStop | scmAcceptShutdown}
}

func statusStopPending(grace time.Duration) scmStatus {
	return scmStatus{State: scmStopPending, WaitHint: uint32((grace + 5*time.Second).Milliseconds())}
}

// scmRun is a Run of a Supervisor over scripted Runners, served by a fake SCM.
type scmRun struct {
	scm       *fakeSCM
	s         *Supervisor
	runners   []*scriptedRunner
	cancelRun context.CancelFunc
	result    chan error
	baseline  int
}

// newSCMRun builds a Supervisor over runners with WithWindowsService and opts whose seams report
// the process as a Windows service and serve it with a fake SCM. It does not run it.
func newSCMRun(t *testing.T, opts []Option, runners ...*scriptedRunner) *scmRun {
	t.Helper()
	r := &scmRun{scm: newFakeSCM(), runners: runners, baseline: runtime.NumGoroutine()}
	rec := &recorder{}
	list := make([]Runner, len(runners))
	for i, sr := range runners {
		sr.rec = rec
		list[i] = sr
	}
	opts = append([]Option{WithLogger(slog.New(slog.DiscardHandler)), WithWindowsService()}, opts...)
	s, err := New(list, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.isWindowsService = func() (bool, error) { return true, nil }
	s.runWindowsService = r.scm.run
	r.s = s
	return r
}

// launch starts Run and returns at once.
func (r *scmRun) launch() *scmRun {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancelRun = cancel
	r.result = make(chan error, 1)
	go func() { r.result <- r.s.Run(ctx) }()
	return r
}

// launchSCM builds and launches a served Run.
func launchSCM(t *testing.T, opts []Option, runners ...*scriptedRunner) *scmRun {
	t.Helper()
	return newSCMRun(t, opts, runners...).launch()
}

// wait returns what Run returned.
func (r *scmRun) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

// finish releases every hung call and checks that no goroutine of the run outlives it.
func (r *scmRun) finish(t *testing.T) {
	t.Helper()
	for _, sr := range r.runners {
		sr.free()
	}
	r.cancelRun()
	settle(t, r.baseline)
}

// startServed launches a served Run over runners and returns once it reports running.
func startServed(t *testing.T, opts []Option, runners ...*scriptedRunner) *scmRun {
	t.Helper()
	r := launchSCM(t, opts, runners...)
	r.scm.expect(t, statusStartPending())
	r.scm.expect(t, statusRunning())
	return r
}

func TestWindowsServiceReportsRunningOnlyOnceReady(t *testing.T) {
	a, b := newScripted("a"), newScripted("b")
	b.hangStart = true
	r := launchSCM(t, nil, a, b)

	r.scm.expect(t, statusStartPending())
	<-a.started
	<-b.started
	r.scm.quiet(t)

	b.free()
	r.scm.expect(t, statusRunning())

	r.scm.send(scmStop)
	r.scm.expect(t, statusStopPending(defaultGrace))
	if code := r.scm.exitCode(t); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if err := r.wait(t); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	r.finish(t)
}

func TestWindowsServiceAnswersInterrogateWithTheCurrentStatus(t *testing.T) {
	a := newScripted("a")
	a.hangStart = true
	b := newScripted("b")
	b.hangStop = true
	r := launchSCM(t, []Option{WithGrace(time.Hour)}, a, b)

	r.scm.expect(t, statusStartPending())
	<-a.started
	r.scm.send(scmInterrogate)
	r.scm.expect(t, statusStartPending())

	a.free()
	<-b.started
	r.scm.expect(t, statusRunning())
	r.scm.send(scmInterrogate)
	r.scm.expect(t, statusRunning())

	r.scm.send(scmStop)
	r.scm.expect(t, statusStopPending(time.Hour))
	<-b.stopCalled
	r.scm.send(scmInterrogate)
	r.scm.expect(t, statusStopPending(time.Hour))
	r.scm.send(scmStop)
	r.scm.expect(t, statusStopPending(time.Hour))

	b.free()
	if code := r.scm.exitCode(t); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if err := r.wait(t); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	r.finish(t)
}

func TestWindowsServiceStopRequestedByTheSCMExitsZero(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		cmd     scmCmd
		grace   time.Duration
		script  func(*scriptedRunner)
		wantErr error
	}{
		{name: "stop", cmd: scmStop, grace: 3 * time.Second, script: func(*scriptedRunner) {}},
		{name: "shutdown", cmd: scmShutdown, grace: 7 * time.Second, script: func(*scriptedRunner) {}},
		{
			name: "stop error", cmd: scmStop, grace: time.Second,
			script:  func(r *scriptedRunner) { r.stopErr = boom },
			wantErr: ErrStopTimeout,
		},
		{
			name: "exit error during stop", cmd: scmShutdown, grace: time.Second,
			script:  func(r *scriptedRunner) { r.stopExit = boom },
			wantErr: boom,
		},
		{
			name: "stop timeout", cmd: scmStop, grace: time.Millisecond,
			script:  func(r *scriptedRunner) { r.hangStop = true },
			wantErr: ErrStopTimeout,
		},
		{
			name: "restart during stop", cmd: scmStop, grace: time.Second,
			script:  func(r *scriptedRunner) { r.stopExit = fmt.Errorf("a: %w", ErrRestartRequired) },
			wantErr: ErrRestartRequired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newScripted("a")
			tt.script(a)
			r := startServed(t, []Option{WithGrace(tt.grace)}, a)

			r.scm.send(tt.cmd)
			r.scm.expect(t, statusStopPending(tt.grace))
			if code := r.scm.exitCode(t); code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			err := r.wait(t)
			if tt.wantErr == nil && err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("Run = %v, want %v", err, tt.wantErr)
			}
			r.scm.quiet(t)
			r.finish(t)
		})
	}
}

func TestWindowsServiceRestartRequestExitsWithTheRestartCode(t *testing.T) {
	a := newScripted("a")
	r := startServed(t, nil, a)

	a.exit(fmt.Errorf("a: %w", ErrRestartRequired))
	if code := r.scm.exitCode(t); code != WindowsRestartExitCode {
		t.Errorf("exit code = %d, want %d", code, WindowsRestartExitCode)
	}
	if err := r.wait(t); !RestartRequested(err) {
		t.Errorf("Run = %v, want a requested restart", err)
	}
	r.scm.quiet(t)
	r.finish(t)
}

func TestWindowsServiceOtherEndingsExitWithTheFailureCode(t *testing.T) {
	boom := errors.New("boom")

	t.Run("runner error", func(t *testing.T) {
		a := newScripted("a")
		r := startServed(t, nil, a)
		a.exit(boom)
		checkFailure(t, r, boom)
	})

	t.Run("runner exits with nil", func(t *testing.T) {
		a := newScripted("a")
		r := startServed(t, nil, a)
		a.exit(nil)
		checkFailure(t, r, ErrRunnerExited)
	})

	t.Run("run context ends", func(t *testing.T) {
		a := newScripted("a")
		r := startServed(t, nil, a)
		r.cancelRun()
		checkFailure(t, r, nil)
	})

	t.Run("start fails before readiness", func(t *testing.T) {
		a := newScripted("a")
		a.startErr = boom
		r := launchSCM(t, nil, a)
		r.scm.expect(t, statusStartPending())
		checkFailure(t, r, boom)
	})
}

// checkFailure fails the test unless the served run ends with the failure code and Run returns an
// error matching want, or nil when want is nil, and nothing is reported after the last status read.
func checkFailure(t *testing.T, r *scmRun, want error) {
	t.Helper()
	if code := r.scm.exitCode(t); code != WindowsFailureExitCode {
		t.Errorf("exit code = %d, want %d", code, WindowsFailureExitCode)
	}
	err := r.wait(t)
	if want == nil && err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if want != nil && !errors.Is(err, want) {
		t.Errorf("Run = %v, want %v", err, want)
	}
	r.scm.quiet(t)
	r.finish(t)
}

func TestWindowsServiceSecondRunReturnsErrAlreadyRanWithoutTheSCM(t *testing.T) {
	// secondRun fails the test unless a second Run of r's Supervisor returns ErrAlreadyRan without
	// asking whether the process is a service or going to the SCM.
	secondRun := func(t *testing.T, r *scmRun) {
		t.Helper()
		r.s.isWindowsService = func() (bool, error) {
			t.Error("isWindowsService called by a second Run")
			return true, nil
		}
		r.s.runWindowsService = func(scmExecute) error {
			t.Error("runWindowsService called by a second Run")
			return nil
		}
		if err := r.s.Run(context.Background()); !errors.Is(err, ErrAlreadyRan) {
			t.Errorf("second Run = %v, want %v", err, ErrAlreadyRan)
		}
	}

	t.Run("after a served run", func(t *testing.T) {
		a := newScripted("a")
		r := startServed(t, nil, a)
		r.scm.send(scmStop)
		r.scm.expect(t, statusStopPending(r.s.grace))
		if code := r.scm.exitCode(t); code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if err := r.wait(t); err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
		secondRun(t, r)
		r.scm.quiet(t)
		r.finish(t)
	})

	t.Run("after the check failed", func(t *testing.T) {
		probe := errors.New("probe")
		a := newScripted("a")
		r := newSCMRun(t, nil, a)
		r.s.isWindowsService = func() (bool, error) { return false, probe }
		r.launch()
		if err := r.wait(t); !errors.Is(err, probe) {
			t.Errorf("Run = %v, want %v", err, probe)
		}
		secondRun(t, r)
		checkNotStarted(t, a)
		r.finish(t)
	})
}

func TestWindowsServiceStartPendingWaitHintIsTheStartTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{time.Second, 2 * time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			a := newScripted("a")
			r := launchSCM(t, []Option{WithStartTimeout(timeout)}, a)
			r.scm.expect(t, scmStatus{State: scmStartPending, WaitHint: uint32(timeout.Milliseconds())})
			r.scm.expect(t, statusRunning())
			r.scm.send(scmStop)
			r.scm.next(t)
			r.scm.exitCode(t)
			if err := r.wait(t); err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
			r.finish(t)
		})
	}
}

func TestWindowsServiceStopPendingWaitHintIsGracePlusMargin(t *testing.T) {
	for _, grace := range []time.Duration{time.Millisecond, 10 * time.Second, 90 * time.Minute} {
		t.Run(grace.String(), func(t *testing.T) {
			// The grace period is measured on a clock that never moves, so a stop that takes longer
			// than a 1ms grace on a slow host still completes: the wait hint is all this test checks.
			a := newScripted("a")
			r := startServed(t, []Option{WithGrace(grace), WithClock(newManualClock())}, a)
			r.scm.send(scmStop)
			got := r.scm.next(t)
			want := uint32((grace + windowsStopMargin).Milliseconds())
			if got.State != scmStopPending || got.WaitHint != want || got.Accepts != 0 {
				t.Errorf("status = %+v, want stop-pending accepting nothing with wait hint %d", got, want)
			}
			r.scm.exitCode(t)
			if err := r.wait(t); err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
			r.finish(t)
		})
	}
}

func TestWaitHintIsCappedAtTheLargestTheSCMTakes(t *testing.T) {
	if got := waitHint(2 * time.Second); got != 2000 {
		t.Errorf("waitHint(2s) = %d, want 2000", got)
	}
	if got, want := waitHint(time.Duration(1<<62)), ^uint32(0); got != want {
		t.Errorf("waitHint(huge) = %d, want %d", got, want)
	}
}

func TestWaitHintOfANegativeDurationIsZero(t *testing.T) {
	for _, d := range []time.Duration{-time.Millisecond, -2 * time.Second, time.Duration(-1 << 62)} {
		if got := waitHint(d); got != 0 {
			t.Errorf("waitHint(%v) = %d, want 0", d, got)
		}
	}
}

func TestWindowsServiceIgnoresAClosedControlChannel(t *testing.T) {
	a := newScripted("a")
	r := newSCMRun(t, nil, a)
	r.s.runWindowsService = func(execute scmExecute) error {
		closed := make(chan scmRequest)
		close(closed)
		r.scm.code <- execute(closed, func(st scmStatus) { r.scm.statuses <- st })
		return nil
	}
	r.launch()
	r.scm.expect(t, statusStartPending())
	r.scm.expect(t, statusRunning())
	a.exit(nil)
	checkFailure(t, r, ErrRunnerExited)
}

func TestWindowsServiceDispatcherErrorIsReturned(t *testing.T) {
	dispatch := errors.New("dispatch")

	t.Run("after the run", func(t *testing.T) {
		a := newScripted("a")
		r := newSCMRun(t, nil, a)
		r.scm.runErr = dispatch
		r.launch()
		r.scm.expect(t, statusStartPending())
		r.scm.expect(t, statusRunning())
		a.exit(fmt.Errorf("a: %w", ErrRestartRequired))
		r.scm.exitCode(t)
		err := r.wait(t)
		if !errors.Is(err, dispatch) || !errors.Is(err, ErrRestartRequired) {
			t.Errorf("Run = %v, want the restart joined with %v", err, dispatch)
		}
		r.finish(t)
	})

	t.Run("without a run", func(t *testing.T) {
		a := newScripted("a")
		r := newSCMRun(t, nil, a)
		r.s.runWindowsService = func(scmExecute) error { return dispatch }
		r.launch()
		if err := r.wait(t); !errors.Is(err, dispatch) {
			t.Errorf("Run = %v, want %v", err, dispatch)
		}
		checkNotStarted(t, a)
		r.finish(t)
	})

	t.Run("no error and no run", func(t *testing.T) {
		a := newScripted("a")
		r := newSCMRun(t, nil, a)
		r.s.runWindowsService = func(scmExecute) error { return nil }
		r.launch()
		if err := r.wait(t); err == nil {
			t.Error("Run = nil, want an error saying the service never started")
		}
		checkNotStarted(t, a)
		r.finish(t)
	})
}

// checkNotStarted fails the test when r's Start was called.
func checkNotStarted(t *testing.T, r *scriptedRunner) {
	t.Helper()
	select {
	case <-r.started:
		t.Error("a Runner was started, want none")
	default:
	}
}

func TestWindowsServiceCheckErrorIsReturnedFromRun(t *testing.T) {
	probe := errors.New("probe")
	a := newScripted("a")
	r := newSCMRun(t, nil, a)
	r.s.isWindowsService = func() (bool, error) { return false, probe }
	r.s.runWindowsService = func(scmExecute) error {
		t.Error("runWindowsService called after the check failed")
		return nil
	}
	r.launch()
	if err := r.wait(t); !errors.Is(err, probe) {
		t.Errorf("Run = %v, want %v", err, probe)
	}
	checkNotStarted(t, a)
	r.finish(t)
}

func TestWindowsServiceOutsideTheSCMRunsAsWithoutIt(t *testing.T) {
	tests := []struct {
		name   string
		option bool
		seams  bool
	}{
		{name: "host seams", option: true},
		{name: "not a service", option: true, seams: true},
		{name: "option not given", seams: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newScripted("a")
			a.rec = &recorder{}
			var opts []Option
			if tt.option {
				opts = append(opts, WithWindowsService())
			}
			s, err := New([]Runner{a}, append(opts, WithLogger(slog.New(slog.DiscardHandler)))...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if tt.seams {
				s.isWindowsService = func() (bool, error) {
					if !tt.option {
						t.Error("isWindowsService called without WithWindowsService")
					}
					return false, nil
				}
				s.runWindowsService = func(scmExecute) error {
					t.Error("runWindowsService called outside the SCM")
					return nil
				}
			}
			baseline := runtime.NumGoroutine()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := make(chan struct{})
			s.ready = func() { close(ready) }
			result := make(chan error, 1)
			go func() { result <- s.Run(ctx) }()

			<-ready
			cancel()
			if err := <-result; err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
			if err := s.Run(context.Background()); !errors.Is(err, ErrAlreadyRan) {
				t.Errorf("second Run = %v, want ErrAlreadyRan", err)
			}
			settle(t, baseline)
		})
	}
}

func TestHostIsWindowsServiceIsFalseForATestProcess(t *testing.T) {
	ok, err := hostIsWindowsService()
	if ok || err != nil {
		t.Errorf("hostIsWindowsService = %v, %v; want false, nil", ok, err)
	}
}

func TestWindowsExitCodes(t *testing.T) {
	if WindowsRestartExitCode != 75 || WindowsFailureExitCode != 1 {
		t.Errorf("codes = %d, %d; want 75, 1, the codes the service package reads",
			WindowsRestartExitCode, WindowsFailureExitCode)
	}
}

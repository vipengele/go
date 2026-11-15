package daemon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// The service-specific exit codes Run reports to the Windows service control manager under
// WithWindowsService, as ADR-0034 ("On Windows a requested restart is a non-zero service exit")
// decides. The SCM does not relaunch a service that stops with exit code 0, so every ending the
// SCM did not ask for is non-zero: the restart code for a requested restart, which the recovery
// action that go.vipengele.org/daemon/service installs answers with a relaunch and its Status
// reads as starting, and the failure code for every other ending.
const (
	// WindowsRestartExitCode is the exit code of a Run whose error RestartRequested reports as a
	// requested restart.
	WindowsRestartExitCode uint32 = 75

	// WindowsFailureExitCode is the exit code of every other ending the SCM did not ask for: a
	// failed start, a Runner's exit, and a Run that returns nil because its context ended.
	WindowsFailureExitCode uint32 = 1
)

// windowsStopMargin is added to the grace period in the wait hint of the stop-pending status. It
// covers the work of the stop phase beyond the Runners' own: releasing the instance lock and
// cancelling the signal subscription.
const windowsStopMargin = 5 * time.Second

// scmCmd is a control the SCM sends, with the values of svc.Cmd.
type scmCmd uint32

const (
	scmStop        scmCmd = 1
	scmInterrogate scmCmd = 4
	scmShutdown    scmCmd = 5
)

// scmState is a service state reported to the SCM, with the values of svc.State.
type scmState uint32

const (
	scmStopped      scmState = 1
	scmStartPending scmState = 2
	scmStopPending  scmState = 3
	scmRunning      scmState = 4
)

// scmAccepts is the set of controls a service accepts, with the bits of svc.Accepted. The SCM
// always delivers Interrogate.
type scmAccepts uint32

const (
	scmAcceptStop     scmAccepts = 1
	scmAcceptShutdown scmAccepts = 4
)

// scmStatus is a status reported to the SCM, the fields of svc.Status the handler sets. WaitHint is
// in milliseconds.
type scmStatus struct {
	State    scmState
	Accepts  scmAccepts
	WaitHint uint32
}

// scmRequest is a control the SCM sends, the fields of svc.ChangeRequest the handler reads.
type scmRequest struct {
	Cmd scmCmd
}

// scmExecute serves one service run: it reads the SCM's controls from requests, reports each
// status through report and returns the service-specific exit code. A nil requests never
// delivers a control.
type scmExecute func(requests <-chan scmRequest, report func(scmStatus)) (exitCode uint32)

// runUnderSCM runs the whole of run under the SCM through runWindowsService: every status, from
// start-pending to stop-pending, is reported from serveSCM, and the exit code it returns reaches
// the SCM with the stopped status. It returns what run returned, and runWindowsService's error
// alongside it when that failed too; when run never ran, it returns runWindowsService's error.
func (s *Supervisor) runUnderSCM(ctx context.Context) error {
	// Buffered, so serveSCM never waits on it, and read only after runWindowsService returns,
	// which the SCM's dispatcher does only once the handler has returned.
	result := make(chan error, 1)
	err := s.runWindowsService(func(requests <-chan scmRequest, report func(scmStatus)) uint32 {
		code, runErr := s.serveSCM(ctx, requests, report)
		result <- runErr
		return code
	})
	select {
	case runErr := <-result:
		if err == nil {
			return runErr
		}
		return errors.Join(runErr, fmt.Errorf("daemon: windows service: %w", err))
	default:
		if err == nil {
			return errors.New("daemon: windows service: the service control manager never started the service")
		}
		return fmt.Errorf("daemon: windows service: %w", err)
	}
}

// serveSCM is the service handler: it runs run in its own goroutine under a context derived from
// ctx and reports its progress to the SCM. It reports start-pending, accepting no control, with a
// wait hint of the start timeout, the bound of the start phase, then running, accepting Stop and
// Shutdown, once run reaches readiness. A Stop or Shutdown reports stop-pending with a wait hint
// of the grace period plus windowsStopMargin and cancels run's context; readiness after it
// reports nothing. Interrogate is answered with the current status.
// It returns the exit code, which windowsExitCode derives, and what run returned.
func (s *Supervisor) serveSCM(ctx context.Context, requests <-chan scmRequest, report func(scmStatus)) (uint32, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ready := make(chan struct{})
	var once sync.Once
	s.serviceReady = func() { once.Do(func() { close(ready) }) }

	current := scmStatus{State: scmStartPending, WaitHint: waitHint(s.startTimeout)}
	report(current)

	done := make(chan error, 1)
	go func() { done <- s.run(runCtx) }()

	stopRequested := false
	for {
		select {
		case <-ready:
			ready = nil
			if !stopRequested {
				current = scmStatus{State: scmRunning, Accepts: scmAcceptStop | scmAcceptShutdown}
				report(current)
			}
		case r, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			switch r.Cmd {
			case scmInterrogate:
				report(current)
			case scmStop, scmShutdown:
				if stopRequested {
					report(current)
					continue
				}
				stopRequested = true
				current = scmStatus{State: scmStopPending, WaitHint: waitHint(s.grace + windowsStopMargin)}
				report(current)
				cancel()
			}
		case err := <-done:
			return windowsExitCode(stopRequested, err), err
		}
	}
}

// windowsExitCode is the service-specific exit code of a run that ended with err: 0 when the SCM
// asked for the stop, whatever err is, WindowsRestartExitCode for a requested restart, and
// WindowsFailureExitCode for any other ending, nil included.
func windowsExitCode(stopRequested bool, err error) uint32 {
	switch {
	case stopRequested:
		return 0
	case RestartRequested(err):
		return WindowsRestartExitCode
	default:
		return WindowsFailureExitCode
	}
}

// waitHint is d in whole milliseconds, capped at the largest wait hint the SCM takes.
func waitHint(d time.Duration) uint32 {
	ms := d.Milliseconds()
	if ms > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(ms)
}

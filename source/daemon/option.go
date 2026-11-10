package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
)

// PanicPolicy decides what a running Runner that exits with a *PanicError means for the other
// Runners. It does not apply to a panic in Start, which is always a start failure, nor to a panic
// during shutdown, which is always a stop problem. The zero value is PanicFail.
type PanicPolicy int

const (
	// PanicFail counts a panic exit as that Runner's failure, which shuts the rest of the Runners
	// down.
	PanicFail PanicPolicy = iota

	// PanicIsolate records the Runner as failed while its siblings keep running. The *PanicError
	// joins into the result of Run.
	PanicIsolate
)

// valid reports whether p is one of the defined policies.
func (p PanicPolicy) valid() bool {
	return p == PanicFail || p == PanicIsolate
}

// Option configures a Supervisor in New. An Option that rejects its argument returns an error,
// which New reports.
type Option func(*Supervisor) error

// WithGrace sets the grace period: how long the Runners have to stop once shutdown begins. The
// default is ten seconds. A grace that is zero or negative is rejected by New.
func WithGrace(grace time.Duration) Option {
	return func(s *Supervisor) error {
		if grace <= 0 {
			return fmt.Errorf("daemon: grace period must be positive, got %v", grace)
		}
		s.grace = grace
		return nil
	}
}

// WithStartTimeout sets the start timeout: how long the whole start phase, every Runner's Start
// in turn, may take. The default is thirty seconds. A timeout that is zero or negative is rejected
// by New.
func WithStartTimeout(timeout time.Duration) Option {
	return func(s *Supervisor) error {
		if timeout <= 0 {
			return fmt.Errorf("daemon: start timeout must be positive, got %v", timeout)
		}
		s.startTimeout = timeout
		return nil
	}
}

// WithClock sets the clock the Supervisor measures its deadlines against: the start timeout, the
// grace period and the deadline of a reload round. The default is the system clock. A nil clock is
// rejected by New.
func WithClock(clock Clock) Option {
	return func(s *Supervisor) error {
		if clock == nil {
			return errors.New("daemon: nil clock")
		}
		s.clock = clock
		return nil
	}
}

// WithReady registers ready, which Run calls once, after every Runner's Start has returned, at the
// moment the Supervisor begins waiting for shutdown. It is not called again while Run waits, after
// a reload round or a panic exit isolated under PanicIsolate, and it is never called when the start
// phase fails or ends early. Run calls it synchronously on its own goroutine, so a ready that
// blocks blocks Run: until it returns, the Supervisor does not notice a Runner exiting or the
// cancellation of Run's context. A ready with work to do starts its own goroutine. A nil ready is
// rejected by New.
func WithReady(ready func()) Option {
	return func(s *Supervisor) error {
		if ready == nil {
			return errors.New("daemon: nil ready")
		}
		s.ready = ready
		return nil
	}
}

// WithSystemd has Run tell systemd how the run goes, through the notification protocol of the
// systemd package, with lookup reading the environment the service manager set, as
// spec.Env.LookupEnv does. The process environment is never read nor changed. A nil lookup is
// rejected by New.
//
// Run sends READY=1 once, at the point WithReady's callback is called, after it returns: after
// every Runner's Start has returned, synchronously on Run's goroutine. It sends STOPPING=1 when
// the stop phase begins, also after a failed start phase, which sends no READY=1. A Run that
// cannot take the instance lock of WithInstanceLock sends neither.
//
// When WATCHDOG_USEC is set, and WATCHDOG_PID, if set, names this process, Run arms a watchdog
// after READY=1: every half of WATCHDOG_USEC, on the clock of WithClock, it sends WATCHDOG=1. With
// WithHealth it first runs the Liveness checks of the Health, each under the Health's check
// timeout, and sends WATCHDOG=1 only when every one passes, so systemd restarts a process that is
// not live; a check that hangs holds the ping back for as long as it runs. Without WithHealth the
// ping is unconditional. The watchdog is disarmed as the stop phase begins, before STOPPING=1,
// and no WATCHDOG=1 is sent after that, even by a tick whose checks were still running; a ping
// already being sent when the stop phase begins can delay it by up to the one-second bound
// systemd.Notify puts on a write. A WATCHDOG_USEC or WATCHDOG_PID that cannot be parsed is logged
// as a warning and leaves the watchdog off.
//
// Outside systemd, when NOTIFY_SOCKET is absent or empty, and on any operating system but Linux,
// nothing is sent and nothing is logged. A notification that fails to send is logged as a warning
// through the Supervisor's logger and never fails Run. WithSystemd keeps its own state: it does not
// replace the callback of WithReady, and both run.
func WithSystemd(lookup func(key string) (string, bool)) Option {
	return func(s *Supervisor) error {
		if lookup == nil {
			return errors.New("daemon: nil systemd lookup")
		}
		s.systemd = lookup
		return nil
	}
}

// WithHealth has New register the Supervisor's health checks in h: supervisor.live, a Liveness
// check, supervisor.ready, a Readiness check, and a Readiness check named after each Runner that
// is a HealthChecker. New registers them as its last step, all or none, so a New that fails leaves
// h as it was. A nil h is rejected by New.
func WithHealth(h *Health) Option {
	return func(s *Supervisor) error {
		if h == nil {
			return errors.New("daemon: nil health")
		}
		s.health = h
		return nil
	}
}

// WithPanicPolicy sets what a panic exit of a running Runner means for its siblings. The default is PanicFail.
// A value that is not a defined PanicPolicy is rejected by New.
func WithPanicPolicy(policy PanicPolicy) Option {
	return func(s *Supervisor) error {
		if !policy.valid() {
			return fmt.Errorf("daemon: unknown panic policy %d", int(policy))
		}
		s.policy = policy
		return nil
	}
}

// WithInstanceLock makes Run hold the instance lock of the tool id, as AcquireInstance takes it
// with opts, for as long as any Runner may be running. Run takes the lock before the first Start
// and releases it once every Runner has been stopped; while another instance holds it, Run starts
// no Runner and returns the *AlreadyRunningError. An identity that fails its Validate is rejected
// by New.
func WithInstanceLock(id spec.Identity, opts paths.Options) Option {
	return func(s *Supervisor) error {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("daemon: instance lock: %w", err)
		}
		s.instance = &instanceLock{id: id, opts: opts}
		return nil
	}
}

// WithLogger sets the logger the Supervisor reports through. Without it Run uses slog.Default(),
// read when Run begins, so a default installed after New is the one used. A nil logger is rejected
// by New.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Supervisor) error {
		if logger == nil {
			return fmt.Errorf("daemon: logger must not be nil")
		}
		s.logger = logger
		return nil
	}
}

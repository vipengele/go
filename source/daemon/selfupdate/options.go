package selfupdate

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.vipengele.org/daemon"
)

// defaultMaxBootAttempts is the number of boots the updated build gets while the update is
// pending: the boot after the last of them rolls back.
const defaultMaxBootAttempts = 3

// Option configures a Machine in New. An Option that rejects its argument returns an error, which
// New reports.
type Option func(*Machine) error

// WithClock sets the clock behind the times a Machine records: StartedAt and UpdatedAt. The default
// is the system clock. A nil clock is rejected by New.
func WithClock(clock daemon.Clock) Option {
	return func(m *Machine) error {
		if clock == nil {
			return errors.New("selfupdate: nil clock")
		}

		m.clock = clock

		return nil
	}
}

// WithMaxBootAttempts sets how many boots the updated build gets while its update is pending. Boot
// counts each one, and the boot that takes the count past n rolls back: with the default of 3, the
// fourth boot of an unconfirmed update restores the backup. An n below 1 is rejected by New.
func WithMaxBootAttempts(n int) Option {
	return func(m *Machine) error {
		if n < 1 {
			return fmt.Errorf("selfupdate: max boot attempts must be at least 1, got %d", n)
		}

		m.maxBootAttempts = n

		return nil
	}
}

// systemClock is the system clock.
type systemClock struct{}

var _ daemon.Clock = systemClock{}

// Now returns time.Now().
func (systemClock) Now() time.Time { return time.Now() }

// AfterFunc runs f after d through time.AfterFunc.
func (systemClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// defaultCycleTimeout bounds one update check of a runner when WithCycleTimeout is not given.
const defaultCycleTimeout = 10 * time.Minute

// defaultRunnerName is the Name of a runner when WithName is not given.
const defaultRunnerName = "selfupdate"

// RunnerOption configures the runner that schedules a daemon's update checks. A RunnerOption that
// rejects its argument returns an error, which the runner's constructor reports.
//
// The runner's clock is set with WithRunnerClock: WithClock sets a Machine's.
type RunnerOption func(*runner) error

// WithLogger sets the logger the runner reports its schedule and its failed checks through. The
// default is slog.Default(), read when the runner is built. A nil logger is rejected.
func WithLogger(logger *slog.Logger) RunnerOption {
	return func(r *runner) error {
		if logger == nil {
			return errors.New("selfupdate: nil logger")
		}

		r.logger = logger

		return nil
	}
}

// WithRunnerClock sets the clock the runner waits on, for the first check, the interval between
// checks and the timeout of each. The default is the system clock. A nil clock is rejected.
func WithRunnerClock(clock daemon.Clock) RunnerOption {
	return func(r *runner) error {
		if clock == nil {
			return errors.New("selfupdate: nil clock")
		}

		r.clock = clock

		return nil
	}
}

// WithName sets the runner's Name, which the Supervisor reports it under. The default is
// "selfupdate". A blank name is rejected.
func WithName(name string) RunnerOption {
	return func(r *runner) error {
		if strings.TrimSpace(name) == "" {
			return errors.New("selfupdate: blank runner name")
		}

		r.name = name

		return nil
	}
}

// WithCycleTimeout bounds one update check: its context ends once d has elapsed on the runner's
// clock. The default is 10 minutes. A d of zero or less is rejected.
func WithCycleTimeout(d time.Duration) RunnerOption {
	return func(r *runner) error {
		if d <= 0 {
			return fmt.Errorf("selfupdate: cycle timeout must be positive, got %v", d)
		}

		r.cycleTimeout = d

		return nil
	}
}

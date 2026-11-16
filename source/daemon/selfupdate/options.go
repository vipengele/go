package selfupdate

import (
	"errors"
	"fmt"
	"log/slog"
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

// WithLogger sets the logger Apply reports through: a post-update hook that failed and a process
// no service supervisor restarts. Without it Apply uses slog.Default(), read when Apply runs. A nil
// logger is rejected by New.
func WithLogger(logger *slog.Logger) Option {
	return func(m *Machine) error {
		if logger == nil {
			return errors.New("selfupdate: nil logger")
		}

		m.logger = logger

		return nil
	}
}

// WithSupervisorCheck sets what Apply's check for a service supervisor reads: label is the name the
// service is installed under, matched against launchd's XPC_SERVICE_NAME, and lookup reads the
// environment as os.LookupEnv does. The check is daemon.UnderSupervisor with the process's own pid;
// when it fails, Apply logs that nothing restarts the process into the installed binary. Without
// this option the label is empty, so launchd never matches, and lookup is os.LookupEnv. A nil
// lookup is rejected by New.
func WithSupervisorCheck(label string, lookup func(string) (string, bool)) Option {
	return func(m *Machine) error {
		if lookup == nil {
			return errors.New("selfupdate: nil environment lookup")
		}

		m.label = label
		m.lookupEnv = lookup

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

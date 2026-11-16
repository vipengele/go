package daemon

import (
	"context"
	"fmt"
	"log/slog"
)

// UpdateGuard judges each boot of a self-updating binary and confirms the update it runs once the
// run has proved itself. *selfupdate.Machine of go.vipengele.org/daemon/selfupdate is one; the
// interface keeps this package from importing it.
type UpdateGuard interface {
	// Boot judges the boot of the running build, before any Runner starts, and reports whether the
	// update it belongs to is rolled back, in which case the process exits so the restored binary
	// is started. An error with false means the boot could not be judged. An error with true means
	// the rollback took place and only its record failed to be stored.
	Boot(ctx context.Context) (bool, error)

	// Confirm records the update the running build belongs to as good. With no update pending it
	// does nothing.
	Confirm(ctx context.Context) error
}

// The messages of the records about the update guard of WithUpdateGuard, both with no "runner" and
// the error under "err": msgUpdateRollbackUnrecorded at Warn for a rollback whose record Boot
// failed to store, which the next boot of the restored build records, and msgUpdateConfirmFailed
// at Error for a Confirm that failed, which leaves the update pending, so the next boot counts
// another attempt.
const (
	msgUpdateRollbackUnrecorded = "update rollback not recorded"
	msgUpdateConfirmFailed      = "update confirm failed"
)

// bootUpdate runs the Boot of the update guard, when there is one, and returns the error that ends
// Run before any Runner starts: Boot's own error when the boot could not be judged, and an error
// wrapping ErrRestartRequired when the update was rolled back. The error of a rollback whose record
// was not stored is logged and left out of the result, which RestartRequested would otherwise not
// report as a restart.
func (s *Supervisor) bootUpdate(ctx context.Context) error {
	if s.updateGuard == nil {
		return nil
	}
	rolledBack, err := s.updateGuard.Boot(ctx)
	if !rolledBack {
		if err != nil {
			return fmt.Errorf("daemon: update guard boot: %w", err)
		}
		return nil
	}
	if err != nil {
		s.log.LogAttrs(ctx, slog.LevelWarn, msgUpdateRollbackUnrecorded, slog.Any("err", err))
	}
	return fmt.Errorf("daemon: update guard rolled the update back: %w", ErrRestartRequired)
}

// confirmUpdate runs the Confirm of the update guard, when there is one, and logs its error, which
// never ends the run.
func (s *Supervisor) confirmUpdate(ctx context.Context) {
	if s.updateGuard == nil {
		return
	}
	if err := s.updateGuard.Confirm(ctx); err != nil {
		s.log.LogAttrs(ctx, slog.LevelError, msgUpdateConfirmFailed, slog.Any("err", err))
	}
}

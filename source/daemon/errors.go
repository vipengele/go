package daemon

import (
	"errors"
	"fmt"
)

// ErrRunnerExited reports that a Runner exited, with a nil Err, before shutdown began.
var ErrRunnerExited = errors.New("daemon: runner exited before shutdown")

// ErrStartTimeout reports that the Runners did not all start within the start timeout.
var ErrStartTimeout = errors.New("daemon: runner start timed out")

// ErrStopTimeout reports that a Runner did not finish stopping within the grace period, or that
// its Stop returned an error.
var ErrStopTimeout = errors.New("daemon: runner stop timed out")

// ErrAlreadyRan reports that a Supervisor was run a second time.
var ErrAlreadyRan = errors.New("daemon: supervisor already ran")

// ErrRestartRequired reports that a Runner asked for the process to be restarted. Test for it with
// RestartRequested, which also looks through joined errors.
var ErrRestartRequired = errors.New("daemon: restart required")

// PanicError reports a panic recovered from a Runner. Value is what was passed to panic and Stack
// is the stack of the panicking goroutine.
type PanicError struct {
	Runner string
	Value  any
	Stack  []byte
}

// Error names the Runner and the panic value.
func (e *PanicError) Error() string {
	return fmt.Sprintf("daemon: runner %q panicked: %v", e.Runner, e.Value)
}

// Unwrap returns the panic value when it is an error, and nil otherwise.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// ErrAlreadyRunning reports that another instance of the tool holds the instance lock. The error
// AcquireInstance returns is an *AlreadyRunningError, which matches it under errors.Is.
var ErrAlreadyRunning = errors.New("daemon: already running")

// AlreadyRunningError reports that another instance holds the instance lock at Path. PID is the
// process id the holder recorded, and 0 when the record is missing or unreadable. The lock alone
// decides which instance runs; PID only describes the holder, and may name a previous holder in
// the moment between a new holder's acquire and its rewrite of the record.
type AlreadyRunningError struct {
	Path string
	PID  int

	err error
}

// Error names the lock file and, when known, the pid of the holder.
func (e *AlreadyRunningError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("daemon: already running (pid %d, lock %s)", e.PID, e.Path)
	}

	return fmt.Sprintf("daemon: already running (lock %s)", e.Path)
}

// Is reports whether target is ErrAlreadyRunning.
func (e *AlreadyRunningError) Is(target error) bool {
	return target == ErrAlreadyRunning
}

// Unwrap returns the error of the lock attempt, which wraps filelock.ErrLocked.
func (e *AlreadyRunningError) Unwrap() error {
	return e.err
}

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

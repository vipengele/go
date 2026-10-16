package daemon

import (
	"errors"
	"fmt"
)

// ErrRunnerExited reports that a Runner's Start returned before its context was cancelled.
var ErrRunnerExited = errors.New("daemon: runner exited before shutdown")

// ErrStopTimeout reports that a Runner's Stop did not return before its context expired.
var ErrStopTimeout = errors.New("daemon: runner stop timed out")

// ErrAlreadyRan reports that a Supervisor was run a second time.
var ErrAlreadyRan = errors.New("daemon: supervisor already ran")

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

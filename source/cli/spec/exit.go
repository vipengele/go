package spec

// CodedError is an error that carries the process exit code a run function asks for. [Coded]
// builds it; a caller finds it anywhere in an error chain with errors.As and reads [CodedError.Code].
// Its text is the wrapped error's text unchanged.
type CodedError struct {
	code int
	err  error
}

// Coded returns err carrying the exit code code. It returns nil when err is nil. The result
// unwraps to err, so errors.Is and errors.As reach through it.
func Coded(code int, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{code: code, err: err}
}

// Code is the exit code the error carries.
func (e *CodedError) Code() int { return e.code }

// Error is the wrapped error's text.
func (e *CodedError) Error() string { return e.err.Error() }

// Unwrap returns the wrapped error.
func (e *CodedError) Unwrap() error { return e.err }

// ReportedError is an error whose message the run function has already shown to the user. A
// caller finds it anywhere in an error chain with errors.As and must not print its own generic
// `tool: message` line for the chain. Its text is the wrapped error's text unchanged.
type ReportedError struct {
	err error
}

// Reported returns err marked as already reported to the user. It returns nil when err is nil.
// The result unwraps to err, so errors.Is and errors.As reach through it.
func Reported(err error) error {
	if err == nil {
		return nil
	}
	return &ReportedError{err: err}
}

// Error is the wrapped error's text.
func (e *ReportedError) Error() string { return e.err.Error() }

// Unwrap returns the wrapped error.
func (e *ReportedError) Unwrap() error { return e.err }

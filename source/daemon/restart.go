package daemon

import "errors"

// RestartRequested reports whether err asks for a clean restart: every leaf of err, which may be
// joined, is or wraps ErrRestartRequired. It is false for nil, for a restart joined with any other
// error such as ErrStopTimeout, and for any error holding a *PanicError, even one wrapping a
// restart, because a panic is never a clean restart.
//
// A consumer's run function turns a requested restart into a clean exit. Exit status 0 lets a
// supervisor with Restart=always (systemd) or KeepAlive (launchd) relaunch the process without
// recording a failure in the journal:
//
//	if daemon.RestartRequested(err) {
//		return nil
//	}
//	return err
func RestartRequested(err error) bool {
	if err == nil {
		return false
	}
	var panicErr *PanicError
	if errors.As(err, &panicErr) {
		return false
	}
	return onlyRestarts(err)
}

// onlyRestarts reports whether every leaf of err is or wraps ErrRestartRequired. A joined error with
// no members has no leaf to match.
func onlyRestarts(err error) bool {
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		children := e.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if child == nil || !onlyRestarts(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		child := e.Unwrap()
		return child != nil && onlyRestarts(child)
	default:
		return errors.Is(err, ErrRestartRequired)
	}
}

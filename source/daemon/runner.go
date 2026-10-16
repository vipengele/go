package daemon

import "context"

// Runner is one unit of long-running work in a daemon. It knows nothing of its sibling Runners or
// of restarts; the Supervisor starts and stops it.
type Runner interface {
	// Name identifies the Runner in errors and logs.
	Name() string

	// Start blocks until ctx is cancelled. Returning nil before ctx is cancelled is a failure of
	// the Runner: the Supervisor treats an early return as the Runner having exited.
	Start(ctx context.Context) error

	// Stop returns once the Runner has released everything it holds, or ctx expires.
	Stop(ctx context.Context) error
}

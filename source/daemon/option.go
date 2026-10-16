package daemon

import (
	"fmt"
	"time"
)

// PanicPolicy decides what a panic in a Runner's Start or Stop means for the other Runners. The
// zero value is PanicFail.
type PanicPolicy int

const (
	// PanicFail recovers a panic in a Runner's Start or Stop into a *PanicError and counts it as
	// that Runner's failure, which shuts the rest of the Runners down.
	PanicFail PanicPolicy = iota

	// PanicIsolate recovers the panic into a *PanicError and records the Runner as failed, while
	// its siblings keep running. The error joins into the result of Run.
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

// WithPanicPolicy sets what a panic in a Runner means for its siblings. The default is PanicFail.
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

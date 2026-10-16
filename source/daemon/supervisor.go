package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// defaultGrace is the grace period when WithGrace is not given.
const defaultGrace = 10 * time.Second

// Supervisor starts a fixed set of Runners together and stops them together. Build one with New.
type Supervisor struct {
	runners []Runner
	grace   time.Duration
	policy  PanicPolicy

	// ran is set by the first Run; a second Run reports ErrAlreadyRan.
	ran atomic.Bool

	// newStopContext returns the context handed to Stop and the function that releases it. It
	// bounds the stop phase by the grace period.
	newStopContext func() (context.Context, context.CancelFunc)
}

// New returns a Supervisor for runners. It rejects an empty list, a nil Runner, a Runner whose
// name is blank, two Runners with the same name, and an Option that fails. The slice is copied, so
// the caller may reuse it.
func New(runners []Runner, opts ...Option) (*Supervisor, error) {
	if len(runners) == 0 {
		return nil, errors.New("daemon: no runners")
	}

	seen := make(map[string]int, len(runners))
	for i, r := range runners {
		if r == nil {
			return nil, fmt.Errorf("daemon: runner %d is nil", i)
		}
		name := r.Name()
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("daemon: runner %d has a blank name", i)
		}
		if first, dup := seen[name]; dup {
			return nil, fmt.Errorf("daemon: runners %d and %d share the name %q", first, i, name)
		}
		seen[name] = i
	}

	s := &Supervisor{
		runners: slices.Clone(runners),
		grace:   defaultGrace,
		policy:  PanicFail,
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("daemon: nil option")
		}
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	grace := s.grace
	s.newStopContext = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), grace)
	}
	return s, nil
}

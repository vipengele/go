package daemon

import (
	"context"
	"fmt"
)

// Reloader is implemented by a Runner that applies a new configuration of type T without
// restarting. Run offers it the configuration loaded by the function given to WithReload[T]; a
// Runner that implements Reloader for another type is not offered it.
//
// Reload runs alongside the Runner's own work, between its Start and its Stop. It never runs at
// the same time as that Runner's Start, Stop or another Reload, except that a Reload abandoned at
// the deadline may still be running while Stop is called, as an abandoned Start can.
//
// A Reload that returns an error, panics or is still running at the round's deadline is fatal to
// the run, like a failed Start: the configuration is not rolled back, the Runner is not offered it
// again and Run begins shutdown.
type Reloader[T any] interface {
	Reload(ctx context.Context, cfg T) error
}

// WithReload makes Run reload its configuration when the process receives SIGHUP. Each time, Run
// calls load once, even when no Runner implements Reloader[T], and offers the result to every
// running Runner that does, in start order, one at a time. The load and every Reload of one round
// share a single deadline: the start timeout, or the end of Run's context. A load that fails or
// does not return before the deadline rejects the configuration: no Runner is called, a Warn is
// logged, every Runner stays on the configuration it has and Run keeps running. A Reload that
// returns an error, panics (under either PanicPolicy) or is still running at the deadline is
// fatal to the run like a failed Start: Run begins shutdown and returns the failure naming the
// Runner, ErrReloadTimeout for a Reload that overran. A deadline that passes between two Reloads
// is ErrReloadTimeout naming no Runner, logged against the Runner whose Reload is not called.
//
// A nil load is rejected by New. Given more than once, the last WithReload wins. On Windows, where
// there is no SIGHUP, a reload never fires. Without WithReload, Run subscribes to no signal.
func WithReload[T any](load func(context.Context) (T, error)) Option {
	return func(s *Supervisor) error {
		if load == nil {
			return fmt.Errorf("daemon: reload load function must not be nil")
		}
		s.reload = &reloadHook{
			accepts: func(r Runner) bool {
				_, ok := r.(Reloader[T])
				return ok
			},
			load: func(ctx context.Context) (offerFunc, error) {
				cfg, err := load(ctx)
				if err != nil {
					return nil, err
				}
				return func(ctx context.Context, r Runner) (bool, error) {
					rl, ok := r.(Reloader[T])
					if !ok {
						return false, nil
					}
					return true, rl.Reload(ctx, cfg)
				}, nil
			},
		}
		return nil
	}
}

// offerFunc offers one loaded configuration to a Runner. It reports false, without calling
// anything, when the Runner does not implement Reloader for the configuration's type, and
// otherwise true with what the Runner's Reload returned.
type offerFunc func(ctx context.Context, r Runner) (offered bool, err error)

// reloadHook is WithReload with its configuration type erased, so a Supervisor needs no type
// parameter. accepts reports whether a Runner implements Reloader for the configuration's type.
// load loads the configuration once and returns the function that offers it to each Runner.
type reloadHook struct {
	accepts func(r Runner) bool
	load    func(ctx context.Context) (offerFunc, error)
}

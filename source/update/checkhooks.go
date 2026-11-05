package update

import (
	"context"
	"fmt"

	"go.vipengele.org/cli/spec"
)

// checkKey is the context key under which the pre-run hook stores the running *Check.
type checkKey struct{}

// UpdateCheckHooks returns the pair of hooks that run the background update check around a
// command. A tool wires them with core.WithPreRun(pre) and core.WithPostRun(post); their types
// are those of core's PreRunHook and PostRunHook, and package update does not import cli/core.
//
// It returns the error of [Config.Validate], with nil hooks, when c is invalid. The hooks keep c
// as given, so a nil IsTerminal is answered from the Invocation's Env, not from the process's
// standard streams.
//
// The pre-run hook leaves a command that carries the [spec.SkipUpdateCheck] annotation alone.
// Otherwise it calls [Config.Start] and returns a context that carries the check; a Start error
// is dropped, since a check never fails a command, and then no check runs.
//
// The post-run hook finishes the check, waiting at most CheckWait. When the context is done it
// cancels the check at once and writes nothing. Otherwise, when the check has a [Notice], it
// writes the notice as one line to the Env's Stderr, whether the command succeeded or failed. The
// terminal is probed once, by the pre-run hook, as one of the gates of [Config.Start]: from
// Config.IsTerminal when set and from the Env's stderr otherwise. A run whose check was ineligible
// has no check to finish, so it writes nothing. It always returns nil: cli/core reports runErr
// itself, and an error from the hook after a successful run would turn it into a failure.
func (c Config) UpdateCheckHooks() (
	pre func(context.Context, *spec.Invocation, spec.CommandInfo) (context.Context, error),
	post func(context.Context, *spec.Invocation, spec.CommandInfo, error) error,
	err error,
) {
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	pre = func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
		if _, skip := cmd.Annotation(spec.SkipUpdateCheck); skip {
			return ctx, nil
		}
		cfg := c
		if cfg.IsTerminal == nil {
			cfg.IsTerminal = func() bool { return inv.Env.IsTerminal(spec.Stderr) }
		}
		k, err := cfg.Start(ctx)
		if err != nil || k == nil {
			return ctx, nil
		}
		return context.WithValue(ctx, checkKey{}, k), nil
	}
	post = func(ctx context.Context, inv *spec.Invocation, _ spec.CommandInfo, _ error) error {
		k, _ := ctx.Value(checkKey{}).(*Check)
		if k == nil {
			return nil
		}
		if ctx.Err() != nil {
			k.cancelNow()
			return nil
		}
		k.Finish()
		if n, ok := k.Notice(); ok {
			_, _ = fmt.Fprintln(inv.Env.Stderr, n.String())
		}
		return nil
	}
	return pre, post, nil
}

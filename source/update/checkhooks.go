package update

import (
	"context"
	"fmt"
	"runtime"

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
// On Windows the pre-run hook first calls [Config.RemoveStaleBackup] on every command, the ones
// carrying the [spec.SkipUpdateCheck] annotation included, so a backup an update left behind
// because the process that ran it held the file open is removed by the next run of the tool. Its
// error is dropped: a locked or missing backup never fails a command. The backup of an update not
// yet settled by [Installed.Discard] or [Installed.Rollback] is the updating process's running
// file there, so the removal fails and Rollback keeps it. On any other host the hook removes no
// backup: an open file can be removed, so a run started between Install and Rollback, by a
// PostUpdate hook or by the user, would take the backup Rollback needs, and there Discard always
// removes the backup.
//
// The hook then leaves a command that carries the [spec.SkipUpdateCheck] annotation alone.
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
		_ = c.removeStaleBackupOnHost(runtime.GOOS)
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

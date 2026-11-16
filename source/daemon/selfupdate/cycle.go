package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"go.vipengele.org/cli/spec/version"
	"go.vipengele.org/daemon"
	"go.vipengele.org/update"
)

// The messages a check logs.
const (
	msgDevelopmentBuild = "self-update disabled: the running build is not a release version"
	msgPendingSkipped   = "self-update check skipped: an update is pending"
	msgNoRelease        = "self-update found no release"
	msgUpToDate         = "self-update found no newer release"
	msgAvailable        = "update available"
	msgBeginPending     = "self-update not installed: another update is pending"
	msgCheckFailed      = "self-update check failed"
	msgApplyFailed      = "self-update install failed"
	msgRestorePending   = "self-update install failed and the previous binary was not restored; the update stays pending for the next boot"
)

var (
	_ update.BackupGuard   = (*Machine)(nil)
	_ daemon.Runner        = (*Runner[struct{}])(nil)
	_ daemon.Reloader[int] = (*Runner[int])(nil)
)

// Runner is the daemon.Runner that keeps a daemon's binary up to date, and the daemon.Reloader[T]
// that applies the Settings a reloaded configuration of type T carries. NewRunner builds it.
//
// Each check, once the jitter of the binary's path and then each Interval has passed:
//
//   - With an update pending in the Machine, logs it at Info and does nothing else.
//   - Asks the Source for the latest release of the Channel. No release (update.ErrNoRelease), or
//     none newer than the running version, is logged at Debug.
//   - With AutoApply off, logs "update available" at Info and downloads nothing.
//   - With AutoApply on, applies the release with Machine.Apply. Once the staged binary has passed
//     every check and before the executable is swapped, the Machine records the update pending,
//     from the running version to the release, with the target and its backup; when another update
//     is pending by then, the install is skipped and logged at Info. A successful install keeps the
//     backup for the Machine's rollback and ends the Runner with the error of Machine.Apply, which
//     matches daemon.ErrRestartRequired; the post-update hooks and the supervisor check of
//     Machine.Apply log through the Machine's logger.
//   - An install that fails after the pending record was written is abandoned by Machine.Apply, so
//     a later check may retry it, and logged at Warn with any failure of the abandon joined on,
//     unless it failed with an *update.RestoreError: the executable may then hold no binary, the
//     record stays pending for the next boot to judge, and the failure is logged at Warn with the
//     backup under "backup".
//
// Every other failure, a refusal by Apply included, is logged at Warn and the next check is
// scheduled as usual; a check ended by its timeout or by Stop is logged at Debug with the context's
// cause, and the scheduler reports a timeout itself.
//
// A build whose running version is not a release version checks nothing: Start logs that at Info
// and schedules no check, and the Runner then runs until Stop.
type Runner[T any] struct {
	sched      *runner
	check      *cycle
	settingsOf func(T) Settings
	release    bool
	version    string

	// idleDone closes when Stop ends a Runner that schedules no check, under mu with idleStarted
	// and idleStopped.
	idleDone    chan struct{}
	mu          sync.Mutex
	idleStarted bool
	idleStopped bool
}

// Name returns the Runner's name, "selfupdate" unless WithName set another.
func (r *Runner[T]) Name() string { return r.sched.Name() }

// Start schedules the first check and returns, or, for a build that is not a release version,
// logs that no check runs and returns. It returns an error when the Runner was already started,
// and one wrapping context.Canceled when Stop came first.
func (r *Runner[T]) Start(ctx context.Context) error {
	if r.release {
		return r.sched.Start(ctx)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idleStarted {
		return fmt.Errorf("selfupdate: runner %q: %w", r.sched.name, errRunnerStarted)
	}
	r.idleStarted = true
	if r.idleStopped {
		return r.sched.stoppedBeforeStart()
	}
	r.sched.logger.LogAttrs(ctx, slog.LevelInfo, msgDevelopmentBuild,
		slog.String("runner", r.sched.name), slog.String("version", r.version))

	return nil
}

// Done is closed once the Runner has ended: after Stop, or after a check that installed an update
// or panicked.
func (r *Runner[T]) Done() <-chan struct{} {
	if r.release {
		return r.sched.Done()
	}
	return r.idleDone
}

// Err returns, once Done is closed, the error of the check that ended the Runner: one matching
// daemon.ErrRestartRequired, or a *daemon.PanicError. It is nil for a Runner Stop ended.
func (r *Runner[T]) Err() error {
	if r.release {
		return r.sched.Err()
	}
	return nil
}

// Stop cancels the next check and the context of a check in flight, and waits for that check to
// return, or for ctx to be done, whose error it then returns. Stop before Start returns nil.
func (r *Runner[T]) Stop(ctx context.Context) error {
	if r.release {
		return r.sched.Stop(ctx)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idleStarted && !r.idleStopped {
		close(r.idleDone)
	}
	r.idleStopped = true

	return nil
}

// Reload applies the Settings settingsOf takes from cfg: AutoApply at once, and a new valid
// Interval to the next check that is waiting. An invalid Interval is logged and the current one
// kept. Reload never returns an error.
func (r *Runner[T]) Reload(_ context.Context, cfg T) error {
	r.sched.reload(r.settingsOf(cfg))
	return nil
}

// cycle is one check of a Runner, over the Config of the binary and the Machine that records its
// updates. sched is the scheduler that calls it, which holds its logger and the current Settings.
type cycle struct {
	cfg     update.Config
	machine *Machine
	sched   *runner
	// install is the installFunc the Machine's Apply runs.
	install installFunc
}

// run checks for a newer release and, when AutoApply is on, installs it, as Runner describes. It
// returns an error wrapping daemon.ErrRestartRequired after an install, and nil otherwise.
func (c *cycle) run(ctx context.Context) error {
	runnerAttr := slog.String("runner", c.sched.name)
	currentAttr := slog.String("version", c.cfg.Version)

	rec, err := c.machine.State(ctx)
	if err != nil {
		c.fail(ctx, msgCheckFailed, err, runnerAttr)
		return nil
	}
	if rec.Status == StatusPending {
		c.log(ctx, slog.LevelInfo, msgPendingSkipped, runnerAttr, currentAttr,
			slog.String("pending_version", rec.ToVersion))
		return nil
	}

	r, err := c.cfg.Resolved()
	if err != nil {
		c.fail(ctx, msgCheckFailed, err, runnerAttr)
		return nil
	}

	rel, err := r.Source.Latest(ctx, r.Channel)
	switch {
	case errors.Is(err, update.ErrNoRelease):
		c.log(ctx, slog.LevelDebug, msgNoRelease, runnerAttr, currentAttr, slog.String("channel", r.Channel.String()))
		return nil
	case err != nil:
		c.fail(ctx, msgCheckFailed, err, runnerAttr)
		return nil
	}

	latestAttr := slog.String("latest", rel.Tag)
	if !version.Newer(r.Version, rel.Tag) {
		c.log(ctx, slog.LevelDebug, msgUpToDate, runnerAttr, currentAttr, latestAttr)
		return nil
	}
	if !c.sched.autoApply() {
		c.log(ctx, slog.LevelInfo, msgAvailable, runnerAttr, currentAttr, latestAttr)
		return nil
	}

	sel, err := r.Select(rel)
	if err != nil {
		c.fail(ctx, msgApplyFailed, err, runnerAttr, latestAttr)
		return nil
	}

	err = c.machine.apply(ctx, r, sel, c.install)

	var restoreErr *update.RestoreError
	switch {
	case daemon.RestartRequested(err):
		return err
	case errors.Is(err, ErrUpdatePending):
		c.log(ctx, slog.LevelInfo, msgBeginPending, runnerAttr, currentAttr, latestAttr)
	case errors.As(err, &restoreErr):
		c.fail(ctx, msgRestorePending, err, runnerAttr, latestAttr, slog.String("backup", restoreErr.Backup))
	default:
		c.fail(ctx, msgApplyFailed, err, runnerAttr, latestAttr)
	}

	return nil
}

// fail logs err with msg: at Warn, or at Debug with the context's cause once ctx has ended, by the
// check's timeout or by Stop, which the scheduler handles.
func (c *cycle) fail(ctx context.Context, msg string, err error, attrs ...slog.Attr) {
	level := slog.LevelWarn
	attrs = append(attrs, slog.Any("err", err))
	if ctx.Err() != nil {
		level = slog.LevelDebug
		attrs = append(attrs, slog.Any("cause", context.Cause(ctx)))
	}
	c.log(ctx, level, msg, attrs...)
}

// log logs msg at level through the scheduler's logger.
func (c *cycle) log(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	c.sched.logger.LogAttrs(ctx, level, msg, attrs...)
}

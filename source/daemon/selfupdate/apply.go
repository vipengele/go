package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"go.vipengele.org/daemon"
	"go.vipengele.org/update"
)

var (
	_ daemon.UpdateGuard = (*Machine)(nil)
	_ update.BackupGuard = (*Machine)(nil)
	_ update.BackupGuard = (*applyGuard)(nil)
)

// The messages of the records Apply logs, both with the release's tag under "version":
// msgPostUpdateFailed at Warn, with the error under "err", for post-update hooks that failed, which
// never fail the update; and msgNotSupervised at Warn, with the label under "label", for a process
// no service supervisor restarts, so the installed binary runs only once something starts it.
const (
	msgPostUpdateFailed = "self-update post-update hook failed"
	msgNotSupervised    = "self-update installed, but no service supervisor restarts the process"
)

// abandonTimeout bounds the Abandon that settles a failed install, which runs even when the
// context of Apply has ended.
const abandonTimeout = 30 * time.Second

// installFunc runs cfg.Apply of sel with opts and then beforeInstall as the last
// update.BeforeInstall hook.
type installFunc func(
	ctx context.Context,
	cfg update.Config,
	sel update.Selection,
	beforeInstall func(context.Context, update.InstallPlan) error,
	opts ...update.ApplyOption,
) (*update.Installed, error)

// configApply is the installFunc of update.Config.Apply.
func configApply(
	ctx context.Context,
	cfg update.Config,
	sel update.Selection,
	beforeInstall func(context.Context, update.InstallPlan) error,
	opts ...update.ApplyOption,
) (*update.Installed, error) {
	return cfg.Apply(ctx, sel, append(opts, update.BeforeInstall(beforeInstall))...)
}

// applyGuard is the update.BackupGuard of one Machine.Apply. It answers as its Machine until began
// is set, once that Apply's Begin has recorded the update pending, and reports the backup not in
// use from then on: the pending record is the install's own, and the guard Install asks before its
// renames must not refuse it. Every other Config keeps the Machine as its guard, so it stays
// refused while the record is pending.
type applyGuard struct {
	m     *Machine
	began atomic.Bool
}

// BackupInUse reports false once the Apply of g has begun, and the Machine's answer before.
func (g *applyGuard) BackupInUse(ctx context.Context) (bool, error) {
	if g.began.Load() {
		return false, nil
	}

	return g.m.BackupInUse(ctx)
}

// Apply installs the release sel names through cfg, recording the update pending before the
// executable is swapped, so that the next boots of the installed build are judged by Boot and
// confirmed by Confirm. It returns an error wrapping daemon.ErrRestartRequired once the update is
// installed: a Runner that returns it ends Supervisor.Run with an error for which
// daemon.RestartRequested is true, and the process exits so its supervisor starts the installed
// binary.
//
// Apply sets cfg.BackupGuard to a guard of its own call that answers as m, so cfg.Apply refuses
// with update.ErrBackupInUse, before any request, while an earlier update is pending. Once the
// staged binary has passed every check, after the update.BeforeInstall hooks of opts, Apply records
// Pending{FromVersion: the running version, ToVersion: sel.Tag, Target: the executable, Backup: its
// backup} with Begin from a hook of its own. From then on the guard of this call reports the backup
// not in use, so the install is not refused by its own record, while every Config carrying m as its
// BackupGuard stays refused. A process that stops between the record and the swap leaves a pending
// update whose binary never started, which the next Boot rolls back without touching a file.
//
// An error of cfg.Apply is returned as cfg.Apply returns it. When Begin refuses, with
// ErrUpdatePending while another update is pending, nothing is installed and nothing is recorded.
// When cfg.Apply fails after Begin, Apply settles the record with Abandon, under a context that
// ctx's cancellation does not reach, bounded by abandonTimeout, and joins an Abandon failure onto
// the error; a failure that is an *update.RestoreError is not abandoned, since the executable may
// then hold no binary, and the record stays pending for Boot to judge the binary that starts.
//
// Once the binary is installed, cfg.PostUpdate runs with the installed path. A hook that fails is
// logged at Warn and never joined into the result, since a restart joined with another error is not
// a restart. Last, Apply checks with daemon.UnderSupervisor, through what WithSupervisorCheck set,
// whether a service supervisor restarts the process, and logs at Warn when none does. The backup is
// kept: Confirm removes it and a rollback restores it.
func (m *Machine) Apply(ctx context.Context, cfg update.Config, sel update.Selection, opts ...update.ApplyOption) error {
	return m.apply(ctx, cfg, sel, configApply, opts...)
}

// apply is Apply through install.
func (m *Machine) apply(
	ctx context.Context,
	cfg update.Config,
	sel update.Selection,
	install installFunc,
	opts ...update.ApplyOption,
) error {
	log := m.logger
	if log == nil {
		log = slog.Default()
	}

	guard := &applyGuard{m: m}
	cfg.BackupGuard = guard

	installed, err := install(ctx, cfg, sel, func(ctx context.Context, plan update.InstallPlan) error {
		if err := m.Begin(ctx, Pending{
			FromVersion: m.running,
			ToVersion:   plan.Version,
			Target:      plan.Target,
			Backup:      plan.Backup,
		}); err != nil {
			return err
		}
		guard.began.Store(true)

		return nil
	}, opts...)
	if err != nil {
		var restoreErr *update.RestoreError
		if !guard.began.Load() || errors.As(err, &restoreErr) {
			return err
		}

		return errors.Join(err, m.abandon(ctx, sel.Tag, err))
	}

	if err := update.RunPostUpdate(cfg.PostUpdate, installed.Path()); err != nil {
		log.LogAttrs(ctx, slog.LevelWarn, msgPostUpdateFailed,
			slog.String("version", sel.Tag), slog.Any("err", err))
	}

	if !daemon.UnderSupervisor(m.lookupEnv, os.Getpid(), m.label) {
		log.LogAttrs(ctx, slog.LevelWarn, msgNotSupervised,
			slog.String("version", sel.Tag), slog.String("label", m.label))
	}

	return fmt.Errorf("selfupdate: %s installed at %s, backup kept at %s, restart to run it: %w",
		sel.Tag, installed.Path(), installed.Backup(), daemon.ErrRestartRequired)
}

// abandon settles the record of an install of tag that failed with cause after Begin recorded it
// pending. It runs on a context of its own, bounded by abandonTimeout, so that an Apply whose
// context has ended still settles the record, and returns Abandon's error wrapped.
func (m *Machine) abandon(ctx context.Context, tag string, cause error) error {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abandonTimeout)
	defer cancel()

	if err := m.Abandon(actx, fmt.Sprintf("installing %s failed: %v", tag, cause)); err != nil {
		return fmt.Errorf("selfupdate: apply: settling the failed install: %w", err)
	}

	return nil
}

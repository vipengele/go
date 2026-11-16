package selfupdate

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.vipengele.org/daemon"
	"go.vipengele.org/update"
)

var (
	_ daemon.UpdateGuard = (*Machine)(nil)
	_ update.BackupGuard = (*Machine)(nil)
)

// The messages of the records Apply logs, both with the release's tag under "version":
// msgPostUpdateFailed at Warn, with the error under "err", for post-update hooks that failed, which
// never fail the update; and msgNotSupervised at Warn, with the label under "label", for a process
// no service supervisor restarts, so the installed binary runs only once something starts it.
const (
	msgPostUpdateFailed = "self-update post-update hook failed"
	msgNotSupervised    = "self-update installed, but no service supervisor restarts the process"
)

// Apply installs the release sel names through cfg and records the update as pending, so that the
// next boots of the installed build are judged by Boot and confirmed by Confirm. It returns an error
// wrapping daemon.ErrRestartRequired once the update is installed and recorded: a Runner that
// returns it ends Supervisor.Run with an error for which daemon.RestartRequested is true, and the
// process exits so its supervisor starts the installed binary.
//
// Apply sets cfg.BackupGuard to m, so cfg.Apply refuses with update.ErrBackupInUse, before any
// request, while an earlier update is pending. Any error of cfg.Apply is returned as it is, with
// nothing installed.
//
// Once the binary is installed, Apply records Pending{FromVersion: the running version, ToVersion:
// sel.Tag, Target: the installed path, Backup: the backup path} with Begin, under a context that
// ctx's cancellation does not reach, since an installed binary with no record is never judged.
// When Begin fails, Apply rolls the install back and returns Begin's error; when the rollback fails
// too, the error carries both and names the backup, where the previous binary stays. The backup is
// otherwise kept: Confirm removes it and a rollback restores it.
//
// cfg.PostUpdate then runs with the installed path. A hook that fails is logged at Warn and never
// joined into the result, since a restart joined with another error is not a restart. Last, Apply
// checks with daemon.UnderSupervisor, through what WithSupervisorCheck set, whether a service
// supervisor restarts the process, and logs at Warn when none does.
func (m *Machine) Apply(ctx context.Context, cfg update.Config, sel update.Selection, opts ...update.ApplyOption) error {
	log := m.logger
	if log == nil {
		log = slog.Default()
	}

	cfg.BackupGuard = m

	installed, err := cfg.Apply(ctx, sel, opts...)
	if err != nil {
		return err
	}

	pending := Pending{
		FromVersion: m.running,
		ToVersion:   sel.Tag,
		Target:      installed.Path(),
		Backup:      installed.Backup(),
	}

	if err := m.Begin(context.WithoutCancel(ctx), pending); err != nil {
		if rbErr := installed.Rollback(); rbErr != nil {
			return fmt.Errorf("selfupdate: apply: recording the update failed: %w; rolling it back failed too, "+
				"the previous binary stays at %s: %w", err, pending.Backup, rbErr)
		}

		return fmt.Errorf("selfupdate: apply: recording the update failed, the install is rolled back: %w", err)
	}

	if err := update.RunPostUpdate(cfg.PostUpdate, pending.Target); err != nil {
		log.LogAttrs(ctx, slog.LevelWarn, msgPostUpdateFailed,
			slog.String("version", sel.Tag), slog.Any("err", err))
	}

	if !daemon.UnderSupervisor(m.lookupEnv, os.Getpid(), m.label) {
		log.LogAttrs(ctx, slog.LevelWarn, msgNotSupervised,
			slog.String("version", sel.Tag), slog.String("label", m.label))
	}

	return fmt.Errorf("selfupdate: %s installed, restart to run it: %w", sel.Tag, daemon.ErrRestartRequired)
}

package daemoncmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon"
	"go.vipengele.org/daemon/selfupdate"
	"go.vipengele.org/daemon/service"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatecmd"
)

// DefaultStartTimeout is how long the update command waits for a restarted service to run when
// [UpdateConfig.StartTimeout] is not set.
const DefaultStartTimeout = 15 * time.Second

// pollInterval is how often the update command reads the state of a restarted service.
const pollInterval = 500 * time.Millisecond

// UpdateConfig is what [NewUpdate] builds the update command of a daemon from.
type UpdateConfig struct {
	// Update describes the tool and where its releases come from, as for
	// [updatecmd.Commands]. Its BackupGuard is replaced by Machine.
	Update update.Config
	// Machine records each installed update as pending, so the daemon's next boots judge and
	// confirm it, and guards the backup of a pending update. It is required.
	Machine *selfupdate.Machine
	// Manager returns the service manager that runs the daemon, called once per installed
	// update. It is required.
	Manager ManagerFunc
	// ServiceBinary is the path the service is installed to run. The command restarts the
	// service only when it resolves, symbolic links followed, to the installed binary.
	ServiceBinary string
	// StartTimeout bounds the wait for a restarted service to run. Zero or less is
	// [DefaultStartTimeout].
	StartTimeout time.Duration
	// Clock times the wait for a restarted service. Nil is the system clock.
	Clock daemon.Clock
}

// NewUpdate returns the update command of a daemon: [updatecmd.Commands] for cfg.Update with
// cfg.Machine as its [update.BackupGuard], so an update is refused with [update.ErrBackupInUse]
// before any download while an earlier one is pending. It panics when cfg.Machine or cfg.Manager
// is nil.
//
// Once a release is installed and the Config's post-update hooks have run, the command records
// the update with [selfupdate.Machine.Begin], from the running version to the installed one, with
// the installed path and its backup, which it keeps for a rollback. When Begin fails, it rolls the
// install back and fails; when the rollback fails too, the error names both failures and the
// backup the previous binary stays at.
//
// The command then brings the service onto the new binary. When the installed binary is not
// [UpdateConfig.ServiceBinary], both resolved with their symbolic links followed, or the service
// manager reports [service.ErrNotInstalled], [service.ErrContainer] or [service.ErrUnsupported],
// or the service is not installed, the command says no service manager runs the binary and
// succeeds. Otherwise it acts on the state the service manager reports:
//
//   - running: it restarts the service and reads its state every 500ms until it runs. A restart
//     that fails, a service that fails or is not installed, and one still not running after
//     [UpdateConfig.StartTimeout] fail the command, with an error naming the version and the
//     state. Running means the process is up, not that the new build has confirmed itself: a
//     build that starts and then crashes in a loop is the daemon's to catch, which restores the
//     previous binary once the build reaches its boot-attempt limit.
//   - stopped: it says the service runs the new version when next started.
//   - starting: it says the next launch runs the new binary.
//   - failed: it says the service must be started by hand.
//   - unknown, or a state the manager cannot read: it warns that the service is to be restarted
//     by hand.
//
// Only a running service is restarted: in every other state the command calls neither Start nor
// Restart, and succeeds.
//
// The backup a pending update keeps is protected only through the BackupGuard of every
// [update.Config] that can remove it. On Windows the pre-run hooks of
// [update.Config.UpdateCheckHooks] remove a stale backup with [update.Config.RemoveStaleBackup],
// so the tool builds one Config with BackupGuard set to the Machine and passes it both to
// NewUpdate and to those hooks. NewUpdate sets the guard on its own copy only and cannot check
// the other.
func NewUpdate(cfg UpdateConfig) []*spec.Command {
	if cfg.Machine == nil {
		panic("daemoncmd: NewUpdate: Machine is nil")
	}
	if cfg.Manager == nil {
		panic("daemoncmd: NewUpdate: Manager is nil")
	}

	u := &updater{
		machine: cfg.Machine,
		manager: cfg.Manager,
		binary:  cfg.ServiceBinary,
		timeout: cfg.StartTimeout,
		clock:   cfg.Clock,
	}
	if u.timeout <= 0 {
		u.timeout = DefaultStartTimeout
	}
	if u.clock == nil {
		u.clock = systemClock{}
	}

	ucfg := cfg.Update
	ucfg.BackupGuard = cfg.Machine
	return updatecmd.Commands(ucfg, updatecmd.WithInstalled(u.installed))
}

// updater is the hook [NewUpdate] hands every installed update to.
type updater struct {
	machine *selfupdate.Machine
	manager ManagerFunc
	binary  string
	timeout time.Duration
	clock   daemon.Clock
}

// installed records a as pending and brings the service onto the installed binary.
func (u *updater) installed(ctx context.Context, inv *spec.Invocation, a updatecmd.Applied) error {
	p := selfupdate.Pending{
		FromVersion: a.From,
		ToVersion:   a.To,
		Target:      a.Installed.Path(),
		Backup:      a.Installed.Backup(),
	}

	// An installed binary with no record is never judged, so a cancelled run still records it.
	if err := u.machine.Begin(context.WithoutCancel(ctx), p); err != nil {
		if rbErr := a.Installed.Rollback(); rbErr != nil {
			return &messageError{
				msg: fmt.Sprintf("%s is installed, but recording the update failed: %v; rolling it back "+
					"failed too, and the previous binary stays at %s: %v", a.To, err, p.Backup, rbErr),
				err: errors.Join(err, rbErr),
			}
		}
		return &messageError{
			msg: fmt.Sprintf("recording the update to %s failed, and it is rolled back: %v", a.To, err),
			err: err,
		}
	}

	return u.restart(ctx, inv, a.To, p.Target)
}

// restart brings the service onto the binary at target, which runs version, as the state the
// service manager reports allows.
func (u *updater) restart(ctx context.Context, inv *spec.Invocation, version, target string) error {
	if !samePath(target, u.binary) {
		unmanaged(inv, target)
		return nil
	}

	mgr, err := u.manager(ctx, inv)
	if err != nil {
		if unmanagedError(err) {
			unmanaged(inv, target)
			return nil
		}
		inv.Warning("the service manager cannot be asked about the service: %v; restart it yourself "+
			"to run %s", err, version)
		return nil
	}

	st, err := mgr.Status(ctx)
	if err != nil {
		if unmanagedError(err) {
			unmanaged(inv, target)
			return nil
		}
		st.State = service.StateUnknown
		st.Detail = joinDetail(st.Detail, err.Error())
	}

	switch st.State {
	case service.StateNotInstalled:
		unmanaged(inv, target)
	case service.StateRunning:
		return u.restartRunning(ctx, inv, mgr, version)
	case service.StateStopped:
		inv.Info("the service is stopped; it runs %s when next started", version)
	case service.StateStarting:
		inv.Info("the service is starting; the next launch runs the new binary")
	case service.StateFailed:
		inv.Warning("the service is %s; start it by hand to run %s", describe(st), version)
	default:
		inv.Warning("the state of the service is %s; restart it yourself to run %s", describe(st), version)
	}
	return nil
}

// restartRunning restarts the running service and waits until it runs again, for at most the
// start timeout.
func (u *updater) restartRunning(ctx context.Context, inv *spec.Invocation, mgr service.Manager, version string) error {
	if err := mgr.Restart(ctx); err != nil {
		return notRunning(version, fmt.Sprintf("restarting it failed while it was running: %v", err), err)
	}

	deadline := u.clock.Now().Add(u.timeout)
	for {
		st, err := mgr.Status(ctx)
		switch {
		case err != nil:
			st.State = service.StateUnknown
			st.Detail = joinDetail(st.Detail, err.Error())
		case st.State == service.StateRunning:
			inv.Info("the service is restarted and runs %s", version)
			return nil
		case st.State == service.StateFailed, st.State == service.StateNotInstalled:
			return notRunning(version, "it is "+describe(st)+" after the restart", nil)
		}

		if !u.clock.Now().Before(deadline) {
			return notRunning(version,
				fmt.Sprintf("it is still %s %s after the restart", describe(st), u.timeout), err)
		}
		if err := u.sleep(ctx, pollInterval); err != nil {
			return notRunning(version,
				fmt.Sprintf("the wait for it ended while it was %s: %v", describe(st), err), err)
		}
	}
}

// sleep waits d on the clock, or until ctx is done.
func (u *updater) sleep(ctx context.Context, d time.Duration) error {
	fired := make(chan struct{})
	stop := u.clock.AfterFunc(d, func() { close(fired) })
	select {
	case <-fired:
		return nil
	case <-ctx.Done():
		stop()
		return ctx.Err()
	}
}

// notRunning is the error of a restarted service that does not run version: what describes what
// was observed, and err, which may be nil, is the error it came with.
func notRunning(version, what string, err error) error {
	return &messageError{
		msg: fmt.Sprintf("%s is installed, but the service does not run it: %s; when the new build "+
			"keeps failing to start, the daemon restores the previous binary once it reaches its "+
			"boot-attempt limit", version, what),
		err: err,
	}
}

// unmanaged says that no service manager restarts the binary at path.
func unmanaged(inv *spec.Invocation, path string) {
	inv.Warning("no service manager runs %s; a running instance keeps the old version and stays "+
		"down once it exits — restart it yourself", path)
}

// unmanagedError reports whether err says that no service manager can run the daemon here.
func unmanagedError(err error) bool {
	return errors.Is(err, service.ErrNotInstalled) ||
		errors.Is(err, service.ErrContainer) ||
		errors.Is(err, service.ErrUnsupported)
}

// describe is the state of st followed by its detail, when it has one.
func describe(st service.Status) string {
	if st.Detail == "" {
		return st.State.String()
	}
	return st.State.String() + " (" + st.Detail + ")"
}

// joinDetail joins two details with "; ", leaving out an empty one.
func joinDetail(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}

// samePath reports whether a and b name the same file once each has its symbolic links followed
// and is made absolute. A path whose links cannot be followed, a missing file among them, is
// compared as it is, made absolute; an empty path matches nothing. Windows compares paths without
// regard to case, as its file systems do.
func samePath(a, b string) bool {
	ra, ok := resolvePath(a)
	if !ok {
		return false
	}
	rb, ok := resolvePath(b)
	if !ok {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ra, rb)
	}
	return ra == rb
}

// resolvePath returns p with its symbolic links followed, when they can be, made absolute, and
// false when p is empty or cannot be made absolute.
func resolvePath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	return abs, true
}

// systemClock is the system clock.
type systemClock struct{}

var _ daemon.Clock = systemClock{}

// Now returns time.Now().
func (systemClock) Now() time.Time { return time.Now() }

// AfterFunc runs f after d through time.AfterFunc.
func (systemClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

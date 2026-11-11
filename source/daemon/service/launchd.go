package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// launchDaemonDir holds the property lists of the LaunchDaemons launchd
	// loads into the system domain.
	launchDaemonDir = "/Library/LaunchDaemons"

	// launchctl exit codes the driver tells apart. launchctl reports a
	// service the domain does not hold as 113 or, on older releases and from
	// bootout, as ESRCH, and both count as not loaded; a bootstrap that races
	// the bootout of the same label fails with EIO.
	launchctlNoSuchProcess = 3
	launchctlIOError       = 5
	launchctlNotFound      = 113

	// launchdUnloadTimeout bounds how long Install and Stop wait for launchd
	// to finish a bootout, and launchdPollInterval is the wait between two
	// looks.
	launchdUnloadTimeout = 5 * time.Second
	launchdPollInterval  = 250 * time.Millisecond
)

// launchd is the Manager of a macOS host. A ScopeSystem service is a
// LaunchDaemon in the system domain, a ScopeUser service a LaunchAgent in the
// gui/<uid> domain of the user; its label is Config.Name. Every contact with
// the operating system goes through a field, so tests substitute fakes and
// the driver compiles and runs its tests on every host.
type launchd struct {
	cfg Config
	// logs is the WithLaunchdLogs setting; an empty path keeps the
	// defaultLaunchdLogs path of that stream.
	logs launchdLogs
	// run starts every command the driver runs: launchctl and id.
	run runner
	// euid is the effective user id of the process.
	euid int
	// uid is the real user id of the process, which names the gui domain of
	// a LaunchAgent.
	uid int
	// lookupEnv reads an environment variable, as os.LookupEnv does.
	lookupEnv func(string) (string, bool)
	// sleep waits d, or returns ctx's error when ctx ends first.
	sleep func(ctx context.Context, d time.Duration) error
	// daemonDir is the directory of LaunchDaemon property lists; empty means
	// /Library/LaunchDaemons.
	daemonDir string
}

func (l *launchd) agent() bool { return l.cfg.Scope == ScopeUser }

// label is the name launchd knows the job by.
func (l *launchd) label() string { return l.cfg.Name }

// domain is the launchd domain the job lives in: system for a LaunchDaemon,
// gui/<uid> for a LaunchAgent.
func (l *launchd) domain() string {
	if l.agent() {
		return "gui/" + strconv.Itoa(l.uid)
	}
	return "system"
}

// target is the service target of the job, <domain>/<label>.
func (l *launchd) target() string { return l.domain() + "/" + l.label() }

// launchdPaths are the files of a job: its property list and its logs.
type launchdPaths struct {
	// plistDir is the directory the property list lives in.
	plistDir string
	// plist is the property list, <plistDir>/<label>.plist.
	plist string
	// logs are the WithLaunchdLogs paths, each empty one replaced by its
	// defaultLaunchdLogs path.
	logs launchdLogs
}

// paths locates the job's files. A LaunchDaemon's property list is in
// daemonDir, by default /Library/LaunchDaemons. A LaunchAgent's is in
// $HOME/Library/LaunchAgents and its default logs are under $HOME, so paths
// fails for a LaunchAgent when HOME is unset or empty.
func (l *launchd) paths() (launchdPaths, error) {
	dir, home := l.daemonDir, ""
	if dir == "" {
		dir = launchDaemonDir
	}
	if l.agent() {
		home, _ = l.lookupEnv("HOME")
		if home == "" {
			return launchdPaths{}, fmt.Errorf("service: HOME is not set, cannot locate the files of the LaunchAgent %s", l.label())
		}
		dir = filepath.Join(home, "Library", "LaunchAgents")
	}
	return launchdPaths{
		plistDir: dir,
		plist:    filepath.Join(dir, l.label()+".plist"),
		logs:     l.logs.withDefaults(defaultLaunchdLogs(l.cfg.Scope, l.label(), home)),
	}, nil
}

// installedPaths returns the job's files, or an error wrapping
// ErrNotInstalled when it has no property list.
func (l *launchd) installedPaths() (launchdPaths, error) {
	p, err := l.paths()
	if err != nil {
		return launchdPaths{}, err
	}
	if _, err := os.Stat(p.plist); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return launchdPaths{}, fmt.Errorf("%w: %s has no plist at %s", ErrNotInstalled, l.label(), p.plist)
		}
		return launchdPaths{}, fmt.Errorf("service: stat plist: %w", err)
	}
	return p, nil
}

// launchctl runs launchctl with args and returns its stdout. A missing
// launchctl returns an error wrapping ErrUnsupported; other errors are the
// runner's, and exitCode reads launchctl's exit status from them.
func (l *launchd) launchctl(ctx context.Context, args ...string) ([]byte, error) {
	out, err := l.run(ctx, append([]string{"launchctl"}, args...))
	if err != nil && errors.Is(err, exec.ErrNotFound) {
		return out, fmt.Errorf("%w: launchctl not found: %w", ErrUnsupported, err)
	}
	return out, err
}

// requirePrivileges fails with an error wrapping ErrPrivilegesRequired when
// the job is a LaunchDaemon and the process is not root. A LaunchAgent needs
// no privileges.
func (l *launchd) requirePrivileges(method string) error {
	if l.agent() || l.euid == 0 {
		return nil
	}
	return fmt.Errorf("%w: launchd %s of the LaunchDaemon %s needs root, run it with sudo",
		ErrPrivilegesRequired, method, l.label())
}

// checkAccount fails when the LaunchDaemon runs as an account that does not
// exist, as the free checkAccount does.
func (l *launchd) checkAccount(ctx context.Context) error {
	if l.agent() || l.cfg.User == "" {
		return nil
	}
	return checkAccount(ctx, l.run, l.cfg.User, l.label())
}

// checkDomain fails with an error wrapping ErrManagerUnreachable when the gui
// domain of a LaunchAgent does not exist, asking `launchctl print gui/<uid>`.
// launchd creates that domain only for a user logged in to the GUI. A
// LaunchDaemon's system domain always exists, and is not asked.
func (l *launchd) checkDomain(ctx context.Context) error {
	if !l.agent() {
		return nil
	}
	_, err := l.launchctl(ctx, "print", l.domain())
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrUnsupported):
		return err
	}
	return fmt.Errorf("%w: the launchd domain %s is unavailable: the LaunchAgent %s needs the user "+
		"logged in to the GUI, which an ssh login or a sudo -u shell is not: %w",
		ErrManagerUnreachable, l.domain(), l.label(), err)
}

// notLoaded reports whether err is launchctl's answer for a service the
// domain does not hold: exit 113, or exit 3 (ESRCH).
func notLoaded(err error) bool {
	code, ok := exitCode(err)
	return ok && (code == launchctlNoSuchProcess || code == launchctlNotFound)
}

// loaded reports whether the domain holds the job, asking
// `launchctl print <domain>/<label>`: exit 0 is loaded, exit 3 or 113 is not.
func (l *launchd) loaded(ctx context.Context) (bool, error) {
	_, err := l.launchctl(ctx, "print", l.target())
	if err == nil {
		return true, nil
	}
	if notLoaded(err) {
		return false, nil
	}
	return false, fmt.Errorf("service: print %s: %w", l.target(), err) // [lydite:exclude_from_mutation][waitUnloaded and Start return the error and discard the bool whenever the error is set, so the value returned with it is unobservable]
}

// bootout unloads the job, stopping its process. A job that is not loaded
// (exit 3 or 113) counts as booted out.
func (l *launchd) bootout(ctx context.Context) error {
	_, err := l.launchctl(ctx, "bootout", l.target())
	if err == nil {
		return nil
	}
	if notLoaded(err) {
		return nil
	}
	return fmt.Errorf("service: bootout %s: %w", l.target(), err)
}

// waitUnloaded looks at the job every launchdPollInterval until the domain no
// longer holds it, and fails once launchdUnloadTimeout has passed. launchd
// finishes a bootout after launchctl returns, and a bootstrap of the label
// before then fails with EIO.
func (l *launchd) waitUnloaded(ctx context.Context) error {
	for waited := time.Duration(0); ; waited += launchdPollInterval {
		loaded, err := l.loaded(ctx)
		if err != nil {
			return err
		}
		if !loaded {
			return nil
		}
		if waited >= launchdUnloadTimeout {
			return fmt.Errorf("service: %s is still loaded %v after bootout", l.target(), launchdUnloadTimeout)
		}
		if err := l.sleep(ctx, launchdPollInterval); err != nil {
			return err
		}
	}
}

// bootstrap loads the job from the property list at path into its domain;
// RunAtLoad then starts it.
func (l *launchd) bootstrap(ctx context.Context, path string) error {
	if _, err := l.launchctl(ctx, "bootstrap", l.domain(), path); err != nil {
		if code, ok := exitCode(err); ok && code == launchctlIOError {
			return fmt.Errorf("service: bootstrap %s: launchd may still be unloading the job: %w", l.target(), err)
		}
		return fmt.Errorf("service: bootstrap %s: %w", l.target(), err)
	}
	return nil
}

// Prepare creates the plist directory and the directories of the log files
// with mode 0750. It needs root for a LaunchDaemon.
func (l *launchd) Prepare(context.Context) error {
	if err := l.requirePrivileges("Prepare"); err != nil {
		return err
	}
	p, err := l.paths()
	if err != nil {
		return err
	}
	return prepareLaunchd(p)
}

// prepareLaunchd creates the directories of the files p names with mode 0750.
func prepareLaunchd(p launchdPaths) error {
	if err := os.MkdirAll(p.plistDir, 0o750); err != nil {
		return fmt.Errorf("service: create plist directory: %w", err)
	}
	for _, f := range []string{p.logs.stdout, p.logs.stderr} {
		if err := os.MkdirAll(filepath.Dir(f), 0o750); err != nil {
			return fmt.Errorf("service: create log directory: %w", err)
		}
	}
	return nil
}

// Install checks privileges, the account and the gui domain, prepares the
// directories, writes the plist, enables the job, boots out any loaded copy,
// waits until launchd has unloaded it and bootstraps the plist. Booting out
// and bootstrapping again is what makes a changed plist or binary take
// effect, as restart does for systemd. A failing bootstrap leaves the plist
// installed and the job not loaded, which Start loads; nothing is rolled back.
func (l *launchd) Install(ctx context.Context) error {
	if err := l.requirePrivileges("Install"); err != nil {
		return err
	}
	if err := l.checkAccount(ctx); err != nil {
		return err
	}
	if err := l.checkDomain(ctx); err != nil {
		return err
	}
	p, err := l.paths()
	if err != nil {
		return err
	}
	if err := prepareLaunchd(p); err != nil {
		return err
	}
	if err := writeFileAtomic(p.plist, "plist", renderPlist(l.cfg, p.logs)); err != nil {
		return err
	}
	if _, err := l.launchctl(ctx, "enable", l.target()); err != nil {
		return fmt.Errorf("service: install %s: enable %s: %w", l.label(), l.target(), err)
	}
	if err := l.bootout(ctx); err != nil {
		return fmt.Errorf("service: install %s: %w", l.label(), err)
	}
	if err := l.waitUnloaded(ctx); err != nil {
		return fmt.Errorf("service: install %s: the plist is installed at %s but the previous job "+
			"is still loaded, run Install again: %w", l.label(), p.plist, err)
	}
	if err := l.bootstrap(ctx, p.plist); err != nil {
		return fmt.Errorf("service: install %s: the plist is installed at %s but not loaded, "+
			"Start retries loading it: %w", l.label(), p.plist, err)
	}
	return nil
}

// Uninstall boots out and disables the job and removes its plist. A failing
// bootout or disable does not keep the plist, and Uninstall then returns an
// error saying which failed. The log files, the service's data and its
// account stay.
func (l *launchd) Uninstall(ctx context.Context) error {
	p, err := l.installed(ctx, "Uninstall")
	if err != nil {
		return err
	}
	bootoutErr := l.bootout(ctx)
	var disableErr error
	if _, err := l.launchctl(ctx, "disable", l.target()); err != nil {
		disableErr = fmt.Errorf("service: disable %s: %w", l.target(), err)
	}
	if err := os.Remove(p.plist); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(fmt.Errorf("service: remove plist: %w", err), bootoutErr, disableErr)
	}
	if err := errors.Join(bootoutErr, disableErr); err != nil {
		return fmt.Errorf("service: uninstall %s: plist removed, unloading failed: %w", l.label(), err)
	}
	return nil
}

// Start kickstarts the job when it is loaded and bootstraps its plist when it
// is not; RunAtLoad then starts it. A running job keeps running.
func (l *launchd) Start(ctx context.Context) error {
	p, err := l.installed(ctx, "Start")
	if err != nil {
		return err
	}
	loaded, err := l.loaded(ctx)
	if err != nil {
		return fmt.Errorf("service: start %s: %w", l.label(), err)
	}
	if !loaded {
		if err := l.bootstrap(ctx, p.plist); err != nil {
			return fmt.Errorf("service: start %s: %w", l.label(), err)
		}
		return nil
	}
	if _, err := l.launchctl(ctx, "kickstart", l.target()); err != nil {
		return fmt.Errorf("service: start %s: kickstart %s: %w", l.label(), l.target(), err)
	}
	return nil
}

// Stop boots out the job and waits until launchd has unloaded it: KeepAlive
// relaunches a job whose process is merely killed. The job stays installed
// and enabled, so it loads again at the next boot or login, or at Start.
// Stopping a job that is not loaded succeeds.
func (l *launchd) Stop(ctx context.Context) error {
	if _, err := l.installed(ctx, "Stop"); err != nil {
		return err
	}
	if err := l.bootout(ctx); err != nil {
		return fmt.Errorf("service: stop %s: %w", l.label(), err)
	}
	if err := l.waitUnloaded(ctx); err != nil {
		return fmt.Errorf("service: stop %s: %w", l.label(), err)
	}
	return nil
}

// installed checks privileges for method, that the plist exists and that the
// job's domain is available, and returns the job's files.
func (l *launchd) installed(ctx context.Context, method string) (launchdPaths, error) {
	if err := l.requirePrivileges(method); err != nil {
		return launchdPaths{}, err
	}
	p, err := l.installedPaths()
	if err != nil {
		return launchdPaths{}, err
	}
	if err := l.checkDomain(ctx); err != nil {
		return launchdPaths{}, err
	}
	return p, nil
}

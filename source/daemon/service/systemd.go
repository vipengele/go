package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const systemUnitDir = "/etc/systemd/system"

// runner runs the command argv and returns its stdout. A failure is an error
// that carries the command's stderr and wraps the underlying error.
type runner func(ctx context.Context, argv []string) ([]byte, error)

// execRunner is the runner that starts argv as a process. It starts only the
// three programs the drivers run, systemctl, launchctl and id, each named by a
// constant, so no input chooses the program; any other argv[0] fails with an
// error wrapping exec.ErrNotFound and starts nothing. A command that exits
// non-zero returns an error wrapping its *exec.ExitError, whose exit code
// exitCode reads.
func execRunner(ctx context.Context, argv []string) ([]byte, error) {
	var cmd *exec.Cmd
	switch argv[0] {
	case "systemctl":
		cmd = exec.CommandContext(ctx, "systemctl", argv[1:]...) // #nosec G204 -- the program is the constant systemctl; its arguments are the driver's fixed verbs and flags plus the validated unit or account name, passed without a shell
	case "launchctl":
		cmd = exec.CommandContext(ctx, "launchctl", argv[1:]...) // #nosec G204 -- the program is the constant launchctl; its arguments are the driver's fixed verbs, the domain built from the uid, the validated label and the plist path under $HOME or /Library/LaunchDaemons, passed without a shell
	case "id":
		cmd = exec.CommandContext(ctx, "id", argv[1:]...) // #nosec G204 -- the program is the constant id; its arguments are the driver's fixed verbs and flags plus the validated unit or account name, passed without a shell
	default:
		return nil, fmt.Errorf("%s: %w: the drivers run only systemctl, launchctl and id", argv[0], exec.ErrNotFound)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.Bytes(), fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, msg)
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return stdout.Bytes(), nil
}

// exitCode returns the exit status carried by an error of the runner, which
// wraps the *exec.ExitError of a command that ran and exited non-zero, and
// false when err carries none.
func exitCode(err error) (int, bool) {
	var e interface{ ExitCode() int }
	if errors.As(err, &e) {
		return e.ExitCode(), true
	}
	return 0, false
}

// systemd is the Manager of a Linux host running systemd. Every contact with
// the operating system goes through a field, so tests substitute fakes.
type systemd struct {
	cfg       Config
	hardening Hardening
	notify    notify
	// run starts every command the driver runs, systemctl and id. New sets
	// execRunner, whose allowlist also holds the launchd driver's launchctl.
	run runner
	// euid is the effective user id of the process.
	euid int
	// lookupEnv reads an environment variable, as os.LookupEnv does.
	lookupEnv func(string) (string, bool)
	// lingerDir is the directory holding one file per user with lingering
	// enabled.
	lingerDir string
	// systemDir is the directory of system unit files; empty means
	// /etc/systemd/system.
	systemDir string
}

func (s *systemd) user() bool { return s.cfg.Scope == ScopeUser }

// unitName is the name of the unit the service manager knows the service by.
func (s *systemd) unitName() string { return s.cfg.Name + ".service" }

// unitDir is the directory the unit file lives in: systemDir, by default
// /etc/systemd/system, for a system unit, $XDG_CONFIG_HOME/systemd/user or $HOME/.config/systemd/user
// for a user unit. It fails when a user unit has neither variable set.
func (s *systemd) unitDir() (string, error) {
	if !s.user() {
		if s.systemDir != "" {
			return s.systemDir, nil
		}
		return systemUnitDir, nil
	}
	if x, ok := s.lookupEnv("XDG_CONFIG_HOME"); ok && x != "" {
		return filepath.Join(x, "systemd", "user"), nil
	}
	if h, ok := s.lookupEnv("HOME"); ok && h != "" {
		return filepath.Join(h, ".config", "systemd", "user"), nil
	}
	return "", errors.New("service: neither XDG_CONFIG_HOME nor HOME is set, cannot locate the user unit directory")
}

// unitPath is the path of the unit file.
func (s *systemd) unitPath() (string, error) {
	dir, err := s.unitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, s.unitName()), nil
}

// systemctl runs systemctl with args, after --user for a user unit, and
// returns its stdout. A missing systemctl returns an error wrapping
// ErrUnsupported; a failure to connect to the manager's bus returns one
// wrapping ErrManagerUnreachable. Other errors are the runner's.
func (s *systemd) systemctl(ctx context.Context, args ...string) ([]byte, error) {
	argv := []string{"systemctl"}
	if s.user() {
		argv = append(argv, "--user")
	}
	argv = append(argv, args...)
	out, err := s.run(ctx, argv)
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, exec.ErrNotFound):
		return out, fmt.Errorf("%w: systemctl not found: %w", ErrUnsupported, err)
	case strings.Contains(err.Error(), "Failed to connect to bus"):
		return out, fmt.Errorf("%w: %w", ErrManagerUnreachable, err)
	}
	return out, err
}

// requirePrivileges fails with an error wrapping ErrPrivilegesRequired when
// the unit is a system unit and the process is not root. A user unit needs no
// privileges.
func (s *systemd) requirePrivileges(method string) error {
	if s.user() || s.euid == 0 {
		return nil
	}
	return fmt.Errorf("%w: systemd %s of the system unit %s needs root, run it with sudo",
		ErrPrivilegesRequired, method, s.unitName())
}

// checkAccount fails when the system unit runs as an account that does not
// exist, as the free checkAccount does.
func (s *systemd) checkAccount(ctx context.Context) error {
	if s.user() || s.cfg.User == "" {
		return nil
	}
	return checkAccount(ctx, s.run, s.cfg.User, s.unitName())
}

// checkAccount fails when account does not exist, asking `id -u -- <account>`
// through run, and names service in the error. The drivers never create the
// account. A missing id returns an error wrapping ErrUnsupported.
func checkAccount(ctx context.Context, run runner, account, service string) error {
	_, err := run(ctx, []string{"id", "-u", "--", account})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("%w: id not found, cannot check the account %q: %w", ErrUnsupported, account, err)
	}
	return fmt.Errorf("service: the account %q does not exist, create it before installing %s: %w",
		account, service, err)
}

// installedUnitPath returns the path of the unit file, or an error wrapping
// ErrNotInstalled when there is none.
func (s *systemd) installedUnitPath() (string, error) {
	path, err := s.unitPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s has no unit file at %s", ErrNotInstalled, s.unitName(), path)
		}
		return "", fmt.Errorf("service: stat unit file: %w", err)
	}
	return path, nil
}

// writeUnit writes the rendered unit to the unit file with writeFileAtomic.
func (s *systemd) writeUnit() error {
	path, err := s.unitPath()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, "unit file", renderUnit(s.cfg, s.hardening, s.notify))
}

// commitUnitFile is commitFile for a unit file.
func commitUnitFile(f *os.File, data []byte, path string) error {
	return commitFile(f, data, path, "unit file")
}

// writeFileAtomic writes data to a temporary file in the directory of path,
// sets its mode to 0644 whatever the umask, and renames it over path, so a
// reader never sees a partial file. The temporary file is removed on any
// failure. noun names the file in every error, such as "unit file" or
// "plist".
func writeFileAtomic(path, noun string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("service: create %s: %w", noun, err)
	}
	if err := commitFile(f, data, path, noun); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}

// commitFile writes data to the temporary file f, sets its mode to 0644,
// closes it and renames it to path. It always closes f, and attempts the
// write, the chmod and the close even after one fails, so the returned error
// joins every failure among them. It never removes f. noun names the file in
// every error.
func commitFile(f *os.File, data []byte, path, noun string) error {
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Chmod(0o644), f.Close()); err != nil {
		return fmt.Errorf("service: write %s: %w", noun, err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("service: rename %s: %w", noun, err)
	}
	return nil
}

// Prepare creates the unit directory with mode 0750. It needs root for a
// system unit.
func (s *systemd) Prepare(context.Context) error {
	if err := s.requirePrivileges("Prepare"); err != nil {
		return err
	}
	dir, err := s.unitDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("service: create unit directory: %w", err)
	}
	return nil
}

// Install checks privileges and the account, prepares the unit directory,
// writes the unit file and then runs daemon-reload, enable and restart. It
// never runs enable --now, which leaves an already running service on its
// old unit; restart starts the service or replaces a running one.
func (s *systemd) Install(ctx context.Context) error {
	if err := s.requirePrivileges("Install"); err != nil {
		return err
	}
	if err := s.checkAccount(ctx); err != nil {
		return err
	}
	if err := s.Prepare(ctx); err != nil {
		return err
	}
	if err := s.writeUnit(); err != nil {
		return err
	}
	unit := s.unitName()
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unit}, {"restart", unit}} {
		if _, err := s.systemctl(ctx, args...); err != nil {
			return fmt.Errorf("service: install %s: %w", unit, err)
		}
	}
	return nil
}

// Uninstall stops and disables the unit, removes its unit file and reloads
// the manager. A failure to disable does not keep the unit file. The
// service's data and its account stay.
func (s *systemd) Uninstall(ctx context.Context) error {
	if err := s.requirePrivileges("Uninstall"); err != nil {
		return err
	}
	path, err := s.installedUnitPath()
	if err != nil {
		return err
	}
	unit := s.unitName()
	_, disableErr := s.systemctl(ctx, "disable", "--now", unit)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(fmt.Errorf("service: remove unit file: %w", err), disableErr)
	}
	if _, err := s.systemctl(ctx, "daemon-reload"); err != nil {
		return errors.Join(fmt.Errorf("service: uninstall %s: %w", unit, err), disableErr)
	}
	if disableErr != nil {
		return fmt.Errorf("service: uninstall %s: unit file removed, disable failed: %w", unit, disableErr)
	}
	return nil
}

// Start starts the installed unit.
func (s *systemd) Start(ctx context.Context) error {
	return s.control(ctx, "Start", "start")
}

// Stop stops the installed unit.
func (s *systemd) Stop(ctx context.Context) error {
	return s.control(ctx, "Stop", "stop")
}

// control runs `systemctl <verb> <unit>` once privileges are checked and the
// unit file exists.
func (s *systemd) control(ctx context.Context, method, verb string) error {
	if err := s.requirePrivileges(method); err != nil {
		return err
	}
	if _, err := s.installedUnitPath(); err != nil {
		return err
	}
	if _, err := s.systemctl(ctx, verb, s.unitName()); err != nil {
		return fmt.Errorf("service: %s %s: %w", verb, s.unitName(), err)
	}
	return nil
}

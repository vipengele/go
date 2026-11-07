package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const systemUnitDir = "/etc/systemd/system"

// runner runs the command argv and returns its stdout. A failure is an error
// that carries the command's stderr and wraps the underlying error.
type runner func(ctx context.Context, argv []string) ([]byte, error)

// execRunner is the runner that starts argv as a process.
func execRunner(ctx context.Context, argv []string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
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

// systemd is the Manager of a Linux host running systemd. Every contact with
// the operating system goes through a field, so tests substitute fakes.
type systemd struct {
	cfg       Config
	hardening Hardening
	// run starts every systemctl command.
	run runner
	// euid is the effective user id of the process.
	euid int
	// lookupEnv reads an environment variable, as os.LookupEnv does.
	lookupEnv func(string) (string, bool)
	// lingerDir is the directory holding one file per user with lingering
	// enabled.
	lingerDir string
}

func (s *systemd) user() bool { return s.cfg.Scope == ScopeUser }

// unitName is the name of the unit the service manager knows the service by.
func (s *systemd) unitName() string { return s.cfg.Name + ".service" }

// unitDir is the directory the unit file lives in: /etc/systemd/system for a
// system unit, $XDG_CONFIG_HOME/systemd/user or $HOME/.config/systemd/user
// for a user unit. It fails when a user unit has neither variable set.
func (s *systemd) unitDir() (string, error) {
	if !s.user() {
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

func (s *systemd) Prepare(context.Context) error { return s.unsupported("Prepare") }

func (s *systemd) Install(context.Context) error { return s.unsupported("Install") }

func (s *systemd) Uninstall(context.Context) error { return s.unsupported("Uninstall") }

func (s *systemd) Start(context.Context) error { return s.unsupported("Start") }

func (s *systemd) Stop(context.Context) error { return s.unsupported("Stop") }

func (s *systemd) Status(context.Context) (Status, error) {
	return Status{}, s.unsupported("Status")
}

func (s *systemd) unsupported(method string) error {
	return fmt.Errorf("%w: systemd %s", ErrUnsupported, method)
}

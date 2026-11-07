package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeRunner records every argv it is given and fails a command whose argv,
// joined by spaces, is a key of fail.
type fakeRunner struct {
	calls [][]string
	fail  map[string]error
}

func (f *fakeRunner) run(_ context.Context, argv []string) ([]byte, error) {
	f.calls = append(f.calls, slices.Clone(argv))
	if err, ok := f.fail[strings.Join(argv, " ")]; ok {
		return nil, err
	}
	return nil, nil
}

// newTestSystemd returns a systemd driver for the unit "demo" whose unit
// directory is under a temporary directory, and the directory itself.
func newTestSystemd(t *testing.T, scope Scope, euid int) (*systemd, *fakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	cfg := Config{Name: "demo", Description: "Demo", Binary: "/usr/bin/demo", Scope: scope}
	if scope == ScopeSystem {
		cfg.User = "demo"
	}
	f := &fakeRunner{fail: map[string]error{}}
	s := &systemd{
		cfg:       cfg,
		hardening: DefaultHardening(scope),
		run:       f.run,
		euid:      euid,
		lookupEnv: func(k string) (string, bool) {
			if k == "XDG_CONFIG_HOME" {
				return filepath.Join(root, "config"), true
			}
			return "", false
		},
		systemDir: filepath.Join(root, "etc", "systemd", "system"),
	}
	dir, err := s.unitDir()
	if err != nil {
		t.Fatalf("unitDir: %v", err)
	}
	return s, f, dir
}

func writeUnitFile(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "demo.service")
	if err := os.WriteFile(path, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSystemdInstallRunsCommandsInOrder(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		euid  int
		want  [][]string
	}{
		{"system", ScopeSystem, 0, [][]string{
			{"id", "-u", "--", "demo"},
			{"systemctl", "daemon-reload"},
			{"systemctl", "enable", "demo.service"},
			{"systemctl", "restart", "demo.service"},
		}},
		{"user", ScopeUser, 1000, [][]string{
			{"systemctl", "--user", "daemon-reload"},
			{"systemctl", "--user", "enable", "demo.service"},
			{"systemctl", "--user", "restart", "demo.service"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, f, _ := newTestSystemd(t, tc.scope, tc.euid)
			if err := s.Install(context.Background()); err != nil {
				t.Fatalf("Install: %v", err)
			}
			if !slices.EqualFunc(f.calls, tc.want, slices.Equal[[]string]) {
				t.Fatalf("commands = %q, want %q", f.calls, tc.want)
			}
			for _, argv := range f.calls {
				if slices.Contains(argv, "--now") {
					t.Fatalf("command %q carries --now", argv)
				}
				if (tc.scope == ScopeUser) != slices.Contains(argv, "--user") && argv[0] == "systemctl" {
					t.Fatalf("command %q: --user must appear exactly for a user unit", argv)
				}
			}
		})
	}
}

func TestSystemdInstallWritesTheRenderedUnit(t *testing.T) {
	for _, scope := range []Scope{ScopeSystem, ScopeUser} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			s, _, dir := newTestSystemd(t, scope, 0)
			if err := s.Install(context.Background()); err != nil {
				t.Fatalf("Install: %v", err)
			}
			path := filepath.Join(dir, "demo.service")
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if want := renderUnit(s.cfg, s.hardening); !bytes.Equal(got, want) {
				t.Fatalf("unit file =\n%s\nwant\n%s", got, want)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o644 {
				t.Fatalf("unit file mode = %v, want 0644", fi.Mode().Perm())
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("unit directory holds %d entries, want only the unit file", len(entries))
			}
		})
	}
}

func TestSystemdSystemUnitNeedsRoot(t *testing.T) {
	methods := map[string]func(*systemd, context.Context) error{
		"Prepare":   (*systemd).Prepare,
		"Install":   (*systemd).Install,
		"Uninstall": (*systemd).Uninstall,
		"Start":     (*systemd).Start,
		"Stop":      (*systemd).Stop,
	}
	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			s, f, dir := newTestSystemd(t, ScopeSystem, 1000)
			err := method(s, context.Background())
			if !errors.Is(err, ErrPrivilegesRequired) {
				t.Fatalf("err = %v, want ErrPrivilegesRequired", err)
			}
			if !strings.Contains(err.Error(), "sudo") {
				t.Fatalf("err = %q, want it to suggest sudo", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("commands = %q, want none", f.calls)
			}
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unit directory stat err = %v, want it absent", err)
			}
		})
	}
}

func TestSystemdUserUnitNeedsNoRoot(t *testing.T) {
	s, _, dir := newTestSystemd(t, ScopeUser, 1000)
	writeUnitFile(t, dir)
	ctx := context.Background()
	for name, err := range map[string]error{"Start": s.Start(ctx), "Stop": s.Stop(ctx)} {
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestSystemdInstallMissingAccount(t *testing.T) {
	s, f, dir := newTestSystemd(t, ScopeSystem, 0)
	f.fail["id -u -- demo"] = errors.New("id -u -- demo: exit status 1: id: 'demo': no such user")
	err := s.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), `"demo"`) {
		t.Fatalf("err = %v, want an error naming the account %q", err, "demo")
	}
	if !slices.EqualFunc(f.calls, [][]string{{"id", "-u", "--", "demo"}}, slices.Equal[[]string]) {
		t.Fatalf("commands = %q, want only the account check", f.calls)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit directory stat err = %v, want it absent", err)
	}
}

func TestSystemdInstallMissingIDIsUnsupported(t *testing.T) {
	s, f, _ := newTestSystemd(t, ScopeSystem, 0)
	f.fail["id -u -- demo"] = fmt.Errorf("id: %w", exec.ErrNotFound)
	if err := s.Install(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestSystemdInstallPassesSystemctlErrorsThrough(t *testing.T) {
	s, f, _ := newTestSystemd(t, ScopeUser, 1000)
	f.fail["systemctl --user daemon-reload"] = errors.New("exit status 1: Failed to connect to bus: No medium found")
	err := s.Install(context.Background())
	if !errors.Is(err, ErrManagerUnreachable) {
		t.Fatalf("err = %v, want ErrManagerUnreachable", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("commands = %q, want to stop after daemon-reload", f.calls)
	}
}

func TestSystemdNotInstalled(t *testing.T) {
	methods := map[string]func(*systemd, context.Context) error{
		"Uninstall": (*systemd).Uninstall,
		"Start":     (*systemd).Start,
		"Stop":      (*systemd).Stop,
	}
	for name, method := range methods {
		for _, scope := range []Scope{ScopeSystem, ScopeUser} {
			t.Run(fmt.Sprint(name, "/", scope), func(t *testing.T) {
				s, f, _ := newTestSystemd(t, scope, 0)
				if err := method(s, context.Background()); !errors.Is(err, ErrNotInstalled) {
					t.Fatalf("err = %v, want ErrNotInstalled", err)
				}
				if len(f.calls) != 0 {
					t.Fatalf("commands = %q, want none", f.calls)
				}
			})
		}
	}
}

func TestSystemdStartStop(t *testing.T) {
	s, f, dir := newTestSystemd(t, ScopeSystem, 0)
	writeUnitFile(t, dir)
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	want := [][]string{{"systemctl", "start", "demo.service"}, {"systemctl", "stop", "demo.service"}}
	if !slices.EqualFunc(f.calls, want, slices.Equal[[]string]) {
		t.Fatalf("commands = %q, want %q", f.calls, want)
	}
}

func TestSystemdUninstall(t *testing.T) {
	tests := []struct {
		name        string
		failDisable bool
	}{
		{"disable succeeds", false},
		{"disable fails", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, f, dir := newTestSystemd(t, ScopeSystem, 0)
			path := writeUnitFile(t, dir)
			if tc.failDisable {
				f.fail["systemctl disable --now demo.service"] = errors.New("exit status 1")
			}
			err := s.Uninstall(context.Background())
			if tc.failDisable != (err != nil) {
				t.Fatalf("err = %v, want an error only when disable fails", err)
			}
			want := [][]string{{"systemctl", "disable", "--now", "demo.service"}, {"systemctl", "daemon-reload"}}
			if !slices.EqualFunc(f.calls, want, slices.Equal[[]string]) {
				t.Fatalf("commands = %q, want %q", f.calls, want)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unit file stat err = %v, want it removed", err)
			}
		})
	}
}

func TestSystemdUnitPathIsEmptyWhenTheUserUnitDirIsUnknown(t *testing.T) {
	s, _, _ := newTestSystemd(t, ScopeUser, 1000)
	s.lookupEnv = func(string) (string, bool) { return "", false }
	if dir, err := s.unitDir(); err == nil || dir != "" {
		t.Fatalf("unitDir = %q, %v, want \"\" and an error", dir, err)
	}
	if path, err := s.unitPath(); err == nil || path != "" {
		t.Fatalf("unitPath = %q, %v, want \"\" and an error", path, err)
	}
}

func TestSystemdInstalledUnitPathIsEmptyWhenNotInstalled(t *testing.T) {
	s, _, _ := newTestSystemd(t, ScopeUser, 1000)
	path, err := s.installedUnitPath()
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("installedUnitPath error = %v, want ErrNotInstalled", err)
	}
	if path != "" {
		t.Fatalf("installedUnitPath = %q, want \"\"", path)
	}
}

func TestSystemdWriteUnitRemovesItsTemporaryFileWhenTheRenameFails(t *testing.T) {
	s, _, dir := newTestSystemd(t, ScopeUser, 1000)
	blocker := filepath.Join(dir, "demo.service")
	if err := os.MkdirAll(filepath.Join(blocker, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.writeUnit(); err == nil {
		t.Fatal("writeUnit = nil, want an error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "demo.service" {
			t.Errorf("leftover file %q in the unit directory", e.Name())
		}
	}
}

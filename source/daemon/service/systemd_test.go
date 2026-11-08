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

func TestSystemdInstalledUnitPathIsEmptyOnAnError(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *systemd)
	}{
		{"unit directory unknown", func(_ *testing.T, s *systemd) {
			s.lookupEnv = func(string) (string, bool) { return "", false }
		}},
		{"unit directory under a regular file", blockUserUnitDir},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := newTestSystemd(t, ScopeUser, 1000)
			tc.setup(t, s)
			path, err := s.installedUnitPath()
			if err == nil || errors.Is(err, ErrNotInstalled) {
				t.Fatalf("installedUnitPath error = %v, want an error other than ErrNotInstalled", err)
			}
			if path != "" {
				t.Fatalf("installedUnitPath = %q, want \"\"", path)
			}
		})
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

// blockUserUnitDir replaces XDG_CONFIG_HOME of the user unit s with a regular
// file, so every path under the unit directory fails with ENOTDIR.
func blockUserUnitDir(t *testing.T, s *systemd) {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s.lookupEnv = func(k string) (string, bool) {
		if k == "XDG_CONFIG_HOME" {
			return blocker, true
		}
		return "", false
	}
}

// fillUnitPath makes the unit path a non-empty directory, which a stat
// accepts and neither a remove nor a rename over it can replace.
func fillUnitPath(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "demo.service")
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSystemdUnknownUserUnitDirFailsEveryMethod(t *testing.T) {
	methods := map[string]func(*systemd, context.Context) error{
		"Prepare":   (*systemd).Prepare,
		"Install":   (*systemd).Install,
		"Uninstall": (*systemd).Uninstall,
		"Start":     (*systemd).Start,
		"Stop":      (*systemd).Stop,
	}
	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			s, f, _ := newTestSystemd(t, ScopeUser, 1000)
			s.lookupEnv = func(string) (string, bool) { return "", false }
			err := method(s, context.Background())
			if err == nil || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
				t.Fatalf("err = %v, want the unknown unit directory", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("commands = %q, want none", f.calls)
			}
		})
	}
	s, _, _ := newTestSystemd(t, ScopeUser, 1000)
	s.lookupEnv = func(string) (string, bool) { return "", false }
	if err := s.writeUnit(); err == nil || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
		t.Fatalf("writeUnit err = %v, want the unknown unit directory", err)
	}
}

func TestSystemdUnreadableUnitDirIsNotNotInstalled(t *testing.T) {
	methods := map[string]func(*systemd, context.Context) error{
		"Uninstall": (*systemd).Uninstall,
		"Start":     (*systemd).Start,
		"Stop":      (*systemd).Stop,
		"Status": func(s *systemd, ctx context.Context) error {
			st, err := s.Status(ctx)
			if st != (Status{State: StateUnknown}) {
				return fmt.Errorf("status = %+v, want only StateUnknown: %w", st, err)
			}
			return err
		},
	}
	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			s, f, _ := newTestSystemd(t, ScopeUser, 1000)
			blockUserUnitDir(t, s)
			err := method(s, context.Background())
			if err == nil || errors.Is(err, ErrNotInstalled) || !strings.Contains(err.Error(), "stat unit file") {
				t.Fatalf("err = %v, want a stat failure other than ErrNotInstalled", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("commands = %q, want none", f.calls)
			}
		})
	}
}

func TestSystemdPrepareFailsWhenTheUnitDirCannotBeCreated(t *testing.T) {
	s, _, _ := newTestSystemd(t, ScopeUser, 1000)
	blockUserUnitDir(t, s)
	err := s.Prepare(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create unit directory") {
		t.Fatalf("err = %v, want the unit directory creation to fail", err)
	}
}

func TestSystemdInstallStopsWhenPrepareFails(t *testing.T) {
	s, f, _ := newTestSystemd(t, ScopeUser, 1000)
	blockUserUnitDir(t, s)
	if err := s.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "create unit directory") {
		t.Fatalf("err = %v, want the unit directory creation to fail", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("commands = %q, want none", f.calls)
	}
}

func TestSystemdInstallStopsWhenTheUnitFileCannotBeWritten(t *testing.T) {
	s, f, dir := newTestSystemd(t, ScopeSystem, 0)
	fillUnitPath(t, dir)
	if err := s.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "rename unit file") {
		t.Fatalf("err = %v, want the unit file rename to fail", err)
	}
	if want := [][]string{{"id", "-u", "--", "demo"}}; !slices.EqualFunc(f.calls, want, slices.Equal[[]string]) {
		t.Fatalf("commands = %q, want only %q", f.calls, want)
	}
}

func TestSystemdInstallStopsAtTheFirstFailingSystemctl(t *testing.T) {
	steps := []string{"systemctl daemon-reload", "systemctl enable demo.service", "systemctl restart demo.service"}
	for i, step := range steps {
		t.Run(step, func(t *testing.T) {
			s, f, _ := newTestSystemd(t, ScopeSystem, 0)
			boom := errors.New("exit status 1")
			f.fail[step] = boom
			err := s.Install(context.Background())
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), "install demo.service") {
				t.Fatalf("err = %v, want the %s failure", err, step)
			}
			// The account check runs first, then every systemctl up to the failing one.
			if len(f.calls) != i+2 || strings.Join(f.calls[len(f.calls)-1], " ") != step {
				t.Fatalf("commands = %q, want to stop after %s", f.calls, step)
			}
		})
	}
}

func TestSystemdControlPassesSystemctlErrorsThrough(t *testing.T) {
	methods := map[string]func(*systemd, context.Context) error{
		"start": (*systemd).Start,
		"stop":  (*systemd).Stop,
	}
	for verb, method := range methods {
		t.Run(verb, func(t *testing.T) {
			s, f, dir := newTestSystemd(t, ScopeSystem, 0)
			writeUnitFile(t, dir)
			boom := errors.New("exit status 5")
			f.fail["systemctl "+verb+" demo.service"] = boom
			err := method(s, context.Background())
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), verb+" demo.service") {
				t.Fatalf("err = %v, want the %s failure", err, verb)
			}
		})
	}
}

func TestSystemdUninstallKeepsGoingWhenTheUnitFileCannotBeRemoved(t *testing.T) {
	for _, failDisable := range []bool{false, true} {
		t.Run(fmt.Sprint("disable fails=", failDisable), func(t *testing.T) {
			s, f, dir := newTestSystemd(t, ScopeSystem, 0)
			path := fillUnitPath(t, dir)
			disableErr := errors.New("disable: exit status 1")
			if failDisable {
				f.fail["systemctl disable --now demo.service"] = disableErr
			}
			err := s.Uninstall(context.Background())
			if err == nil || !strings.Contains(err.Error(), "remove unit file") {
				t.Fatalf("err = %v, want the unit file removal to fail", err)
			}
			if errors.Is(err, disableErr) != failDisable {
				t.Fatalf("err = %v, want it to carry the disable failure exactly when disable fails", err)
			}
			if want := [][]string{{"systemctl", "disable", "--now", "demo.service"}}; !slices.EqualFunc(f.calls, want, slices.Equal[[]string]) {
				t.Fatalf("commands = %q, want only %q", f.calls, want)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("unit path stat err = %v, want it kept", err)
			}
		})
	}
}

func TestSystemdUninstallReportsAFailedReload(t *testing.T) {
	for _, failDisable := range []bool{false, true} {
		t.Run(fmt.Sprint("disable fails=", failDisable), func(t *testing.T) {
			s, f, dir := newTestSystemd(t, ScopeSystem, 0)
			path := writeUnitFile(t, dir)
			reloadErr := errors.New("reload: exit status 1")
			disableErr := errors.New("disable: exit status 1")
			f.fail["systemctl daemon-reload"] = reloadErr
			if failDisable {
				f.fail["systemctl disable --now demo.service"] = disableErr
			}
			err := s.Uninstall(context.Background())
			if !errors.Is(err, reloadErr) || !strings.Contains(err.Error(), "uninstall demo.service") {
				t.Fatalf("err = %v, want the daemon-reload failure", err)
			}
			if errors.Is(err, disableErr) != failDisable {
				t.Fatalf("err = %v, want it to carry the disable failure exactly when disable fails", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unit file stat err = %v, want it removed", err)
			}
		})
	}
}

func TestSystemdWriteUnitFailsWhenTheUnitDirIsMissing(t *testing.T) {
	s, _, dir := newTestSystemd(t, ScopeUser, 1000)
	err := s.writeUnit()
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "create unit file") {
		t.Fatalf("err = %v, want the temporary file creation to fail", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit directory stat err = %v, want it still absent", err)
	}
}

func TestCommitUnitFileJoinsEveryWriteFailureAndKeepsTheTarget(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "demo.service")
	err = commitUnitFile(f, []byte("[Unit]\n"), target)
	if !errors.Is(err, os.ErrClosed) || !strings.Contains(err.Error(), "write unit file") {
		t.Fatalf("err = %v, want a write failure wrapping os.ErrClosed", err)
	}
	j, ok := errors.Unwrap(err).(interface{ Unwrap() []error })
	if !ok || len(j.Unwrap()) != 3 {
		t.Fatalf("err = %v, want the write, chmod and close failures joined", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target stat err = %v, want no unit file", err)
	}
	if data, err := os.ReadFile(f.Name()); err != nil || len(data) != 0 {
		t.Fatalf("temporary file = %q, %v, want it kept and empty", data, err)
	}
}

func TestCommitUnitFileWritesWithMode0644AndRenames(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "unit")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "demo.service")
	if err := commitUnitFile(f, []byte("[Unit]\n"), target); err != nil {
		t.Fatalf("commitUnitFile: %v", err)
	}
	if err := f.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("second close err = %v, want os.ErrClosed, the file already closed", err)
	}
	if _, err := os.Stat(f.Name()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file stat err = %v, want it renamed away", err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", fi.Mode().Perm())
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "[Unit]\n" {
		t.Fatalf("unit file = %q, %v, want the data", data, err)
	}
}

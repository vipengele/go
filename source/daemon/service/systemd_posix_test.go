// The tests in this file rest on POSIX file systems: the permission bits of a
// file or directory, POSIX absolute paths, and ENOTDIR, which Windows reports
// as a missing path, for a path through a regular file.

//go:build !windows

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
			if want := renderUnit(s.cfg, s.hardening, s.notify); !bytes.Equal(got, want) {
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

func TestSystemdUnitPath(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		env   map[string]string
		want  string
		fails bool
	}{
		{"system", ScopeSystem, nil, "/etc/systemd/system/x.service", false},
		{"xdg", ScopeUser, map[string]string{"XDG_CONFIG_HOME": "/c", "HOME": "/h"}, "/c/systemd/user/x.service", false},
		{"home", ScopeUser, map[string]string{"HOME": "/h"}, "/h/.config/systemd/user/x.service", false},
		{"neither", ScopeUser, nil, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &systemd{cfg: Config{Name: "x", Scope: tc.scope}, lookupEnv: env(tc.env)}
			got, err := s.unitPath()
			if tc.fails != (err != nil) || got != tc.want {
				t.Fatalf("unitPath = %q, %v; want %q, fails=%v", got, err, tc.want, tc.fails)
			}
		})
	}
}

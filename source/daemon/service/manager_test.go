package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestNewInvalidConfig(t *testing.T) {
	m, err := New(Config{}, WithGOOS("linux"))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, must not match ErrUnsupported", err)
	}
	if m != nil {
		t.Fatalf("Manager = %v, want nil", m)
	}
}

func TestNewUnsupported(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "plan9"} {
		t.Run(goos, func(t *testing.T) {
			m, err := New(validConfig(goos), WithGOOS(goos))
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, must not match ErrInvalidConfig", err)
			}
			if !strings.Contains(err.Error(), goos) {
				t.Fatalf("err = %q, want it to name %q", err, goos)
			}
			if m != nil {
				t.Fatalf("Manager = %v, want nil", m)
			}
		})
	}
}

func TestNewLinuxReturnsTheSystemdDriver(t *testing.T) {
	m, err := New(validConfig("linux"), WithGOOS("linux"))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if m == nil {
		t.Fatal("Manager = nil, want the systemd driver")
	}
}

func TestSystemctlMapsErrors(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		err   error
		want  error
		argv  []string
	}{
		{"system", ScopeSystem, nil, nil, []string{"systemctl", "start", "x.service"}},
		{"user prefix", ScopeUser, nil, nil, []string{"systemctl", "--user", "start", "x.service"}},
		{"not found", ScopeSystem, fmt.Errorf("run: %w", exec.ErrNotFound), ErrUnsupported, nil},
		{"no bus", ScopeUser, errors.New("exit status 1: Failed to connect to bus: No medium found"), ErrManagerUnreachable, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			s := &systemd{
				cfg: Config{Name: "x", Scope: tc.scope},
				run: func(_ context.Context, argv []string) ([]byte, error) {
					got = argv
					return nil, tc.err
				},
			}
			_, err := s.systemctl(context.Background(), "start", "x.service")
			if tc.want == nil {
				if err != nil || !slices.Equal(got, tc.argv) {
					t.Fatalf("err = %v, argv = %v, want nil, %v", err, got, tc.argv)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewWithGOOSDrivesValidation(t *testing.T) {
	cfg := validConfig("linux")
	cfg.Binary = "/usr/bin/x"
	cfg.WorkingDir = ""
	cfg.User = ""
	m, err := New(cfg, WithGOOS("windows"))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	if m != nil {
		t.Fatalf("Manager = %v, want nil", m)
	}
}

func TestNewDefaultsToRuntimeGOOS(t *testing.T) {
	cfg := validConfig(runtime.GOOS)
	_, err := New(cfg)
	_, errExplicit := New(cfg, WithGOOS(runtime.GOOS))
	_, errEmpty := New(cfg, WithGOOS(""))
	if fmt.Sprint(err) != fmt.Sprint(errExplicit) || fmt.Sprint(err) != fmt.Sprint(errEmpty) {
		t.Fatalf("default = %v, explicit = %v, empty = %v; want equal", err, errExplicit, errEmpty)
	}
	if err != nil && !strings.Contains(err.Error(), runtime.GOOS) {
		t.Fatalf("err = %q, want it to name %q", err, runtime.GOOS)
	}
}

func TestSystemdUnitPath(t *testing.T) {
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
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

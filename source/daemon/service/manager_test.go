package service

import (
	"errors"
	"runtime"
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
	for _, goos := range []string{"linux", "darwin", "windows", "plan9"} {
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
	if err == nil || err.Error() != errExplicit.Error() || err.Error() != errEmpty.Error() {
		t.Fatalf("default = %v, explicit = %v, empty = %v; want equal", err, errExplicit, errEmpty)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Fatalf("err = %q, want it to name %q", err, runtime.GOOS)
	}
}

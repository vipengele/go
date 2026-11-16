//go:build !windows

package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestHostSCMIsUnsupported(t *testing.T) {
	s := hostSCM()
	ok, err := s.elevated()
	if ok || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("elevated = %v, %v; want false, ErrUnsupported", ok, err)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Fatalf("err = %q, want it to name %q", err, runtime.GOOS)
	}
	for _, access := range []scmAccess{scmQuery, scmControl} {
		m, err := s.connect(access)
		if m != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatalf("connect(%d) = %v, %v; want nil, ErrUnsupported", access, m, err)
		}
	}
}

func TestNewWindowsOnAnotherHostFailsEveryMethodUnsupported(t *testing.T) {
	cfg := validConfig("windows")
	cfg.WorkingDir = ""
	m, err := New(cfg, WithGOOS("windows"), notInContainer())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if _, ok := m.(*windowsDriver); !ok {
		t.Fatalf("Manager = %T, want *windowsDriver", m)
	}
	ctx := context.Background()
	methods := map[string]func() error{
		"Prepare":   func() error { return m.Prepare(ctx) },
		"Install":   func() error { return m.Install(ctx) },
		"Uninstall": func() error { return m.Uninstall(ctx) },
		"Start":     func() error { return m.Start(ctx) },
		"Stop":      func() error { return m.Stop(ctx) },
		"Status": func() error {
			st, err := m.Status(ctx)
			if st.State != StateUnknown {
				return fmt.Errorf("State = %v, want %v (err %v)", st.State, StateUnknown, err)
			}
			return err
		},
	}
	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
		})
	}
}

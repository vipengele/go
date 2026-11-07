package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// showRunner answers every command with out and err, and records each argv.
type showRunner struct {
	calls [][]string
	out   string
	err   error
}

func (r *showRunner) run(_ context.Context, argv []string) ([]byte, error) {
	r.calls = append(r.calls, slices.Clone(argv))
	return []byte(r.out), r.err
}

// newStatusSystemd returns an installed "demo" unit of scope whose commands
// answer with out and err. USER is "alice" and the linger directory is empty
// unless lingering is true, when it holds a file for alice.
func newStatusSystemd(t *testing.T, scope Scope, out string, err error, lingering bool) (*systemd, *showRunner) {
	t.Helper()
	s, _, dir := newTestSystemd(t, scope, 1000)
	writeUnitFile(t, dir)
	r := &showRunner{out: out, err: err}
	s.run = r.run
	env := s.lookupEnv
	s.lookupEnv = func(k string) (string, bool) {
		if k == "USER" {
			return "alice", true
		}
		return env(k)
	}
	s.lingerDir = t.TempDir()
	if lingering {
		if err := os.WriteFile(filepath.Join(s.lingerDir, "alice"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return s, r
}

// show renders a systemctl show answer; an empty value omits the key.
func show(load, active, result, restarts, fileState string) string {
	var b strings.Builder
	for _, kv := range [][2]string{
		{"LoadState", load},
		{"ActiveState", active},
		{"SubState", "running"},
		{"Result", result},
		{"MainPID", "42"},
		{"NRestarts", restarts},
		{"UnitFileState", fileState},
	} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "%s=%s\n", kv[0], kv[1])
		}
	}
	return b.String()
}

func TestSystemdStatusMapsShowOutput(t *testing.T) {
	tests := []struct {
		name         string
		out          string
		want         State
		restarts     int
		detail       string
		detailAbsent bool
	}{
		{"active", show("loaded", "active", "success", "0", "enabled"), StateRunning, 0, "", true},
		{"reloading", show("loaded", "reloading", "success", "0", "enabled"), StateRunning, 0, "", true},
		{"activating", show("loaded", "activating", "success", "0", "enabled"), StateStarting, 0, "", true},
		{"inactive", show("loaded", "inactive", "success", "0", "enabled"), StateStopped, 0, "", true},
		{"deactivating", show("loaded", "deactivating", "success", "0", "enabled"), StateStopped, 0, "", true},
		{"failed", show("loaded", "failed", "exit-code", "0", "enabled"), StateFailed, 0, "journalctl -u demo.service", false},
		{"unknown active state", show("loaded", "maintenance", "success", "0", "enabled"), StateUnknown, 0, "ActiveState", false},
		{"crash loop", show("loaded", "activating", "exit-code", "3", "enabled"), StateFailed, 3, "journalctl -u demo.service", false},
		{"restart after a clean exit", show("loaded", "activating", "success", "2", "enabled"), StateStarting, 2, "", true},
		{"missing NRestarts", show("loaded", "activating", "exit-code", "", "enabled"), StateStarting, 0, "", true},
		{"garbage NRestarts", show("loaded", "active", "success", "many", "enabled"), StateUnknown, 0, "NRestarts", false},
		{"not found", show("not-found", "inactive", "success", "0", ""), StateUnknown, 0, "not-found", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStatusSystemd(t, ScopeSystem, tc.out, nil, false)
			st, err := s.Status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st.State != tc.want {
				t.Fatalf("State = %v, want %v (detail %q)", st.State, tc.want, st.Detail)
			}
			if st.Restarts != tc.restarts {
				t.Fatalf("Restarts = %d, want %d", st.Restarts, tc.restarts)
			}
			if tc.detailAbsent && st.Detail != "" {
				t.Fatalf("Detail = %q, want none", st.Detail)
			}
			if !strings.Contains(st.Detail, tc.detail) {
				t.Fatalf("Detail = %q, want it to mention %q", st.Detail, tc.detail)
			}
			if tc.want == StateRunning && st.PID != 42 {
				t.Fatalf("PID = %d, want 42", st.PID)
			}
		})
	}
}

func TestSystemdStatusCrashLoopOfAUserUnitPointsToTheUserJournal(t *testing.T) {
	s, _ := newStatusSystemd(t, ScopeUser, show("loaded", "activating", "exit-code", "3", "enabled"), nil, true)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateFailed || !strings.Contains(st.Detail, "journalctl --user -u demo.service") {
		t.Fatalf("Status = %+v, want failed pointing to journalctl --user", st)
	}
}

func TestSystemdStatusRunsOneShowCommand(t *testing.T) {
	props := "--property=LoadState,ActiveState,SubState,Result,MainPID,NRestarts,UnitFileState"
	tests := []struct {
		scope Scope
		want  []string
	}{
		{ScopeSystem, []string{"systemctl", "show", "demo.service", props}},
		{ScopeUser, []string{"systemctl", "--user", "show", "demo.service", props}},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.scope), func(t *testing.T) {
			s, r := newStatusSystemd(t, tc.scope, show("loaded", "active", "success", "0", "enabled"), nil, true)
			if _, err := s.Status(context.Background()); err != nil {
				t.Fatalf("Status: %v", err)
			}
			if !slices.EqualFunc(r.calls, [][]string{tc.want}, slices.Equal[[]string]) {
				t.Fatalf("commands = %q, want %q", r.calls, [][]string{tc.want})
			}
		})
	}
}

func TestSystemdStatusNotInstalledRunsNoCommand(t *testing.T) {
	for _, scope := range []Scope{ScopeSystem, ScopeUser} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			s, f, _ := newTestSystemd(t, scope, 1000)
			st, err := s.Status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st != (Status{State: StateNotInstalled}) {
				t.Fatalf("Status = %+v, want only StateNotInstalled", st)
			}
			if len(f.calls) != 0 {
				t.Fatalf("commands = %q, want none", f.calls)
			}
		})
	}
}

func TestSystemdStatusUnreachableManager(t *testing.T) {
	busErr := errors.New("systemctl --user show: exit status 1: Failed to connect to bus: No medium found")
	s, _ := newStatusSystemd(t, ScopeUser, "", busErr, true)
	st, err := s.Status(context.Background())
	if !errors.Is(err, ErrManagerUnreachable) {
		t.Fatalf("Status error = %v, want ErrManagerUnreachable", err)
	}
	if st.State != StateUnknown || !strings.Contains(st.Detail, "unreachable") || !strings.Contains(st.Detail, "sudo -u") {
		t.Fatalf("Status = %+v, want unknown explaining the unreachable user manager", st)
	}
}

func TestSystemdStatusPassesOtherErrorsThrough(t *testing.T) {
	boom := errors.New("boom")
	s, _ := newStatusSystemd(t, ScopeSystem, "", boom, false)
	st, err := s.Status(context.Background())
	if !errors.Is(err, boom) || errors.Is(err, ErrManagerUnreachable) {
		t.Fatalf("Status error = %v, want boom alone", err)
	}
	if st.State != StateUnknown {
		t.Fatalf("State = %v, want unknown", st.State)
	}
}

func TestSystemdStatusHasNoDetailWhenLingeringIsOn(t *testing.T) {
	s, _ := newStatusSystemd(t, ScopeUser, show("loaded", "active", "success", "0", "enabled"), nil, true)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Detail != "" {
		t.Fatalf("Detail = %q, want it empty", st.Detail)
	}
}

func TestSystemdStatusLinger(t *testing.T) {
	tests := []struct {
		name      string
		scope     Scope
		lingering bool
		out       string
		wantNote  bool
		want      string
	}{
		{"user unit, lingering off", ScopeUser, false, show("loaded", "active", "success", "0", "enabled"), true, ""},
		{"user unit, lingering on", ScopeUser, true, show("loaded", "active", "success", "0", "enabled"), false, ""},
		{"system unit, lingering off", ScopeSystem, false, show("loaded", "active", "success", "0", "enabled"), false, ""},
		{"user unit, lingering off, failed", ScopeUser, false, show("loaded", "failed", "exit-code", "0", "enabled"), true, "journalctl --user"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStatusSystemd(t, tc.scope, tc.out, nil, tc.lingering)
			st, err := s.Status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if got := strings.Contains(st.Detail, "lingering is off"); got != tc.wantNote {
				t.Fatalf("Detail = %q, linger note present = %v, want %v", st.Detail, got, tc.wantNote)
			}
			if tc.wantNote && !strings.Contains(st.Detail, "loginctl enable-linger alice") {
				t.Fatalf("Detail = %q, want it to name loginctl enable-linger alice", st.Detail)
			}
			if !strings.Contains(st.Detail, tc.want) {
				t.Fatalf("Detail = %q, want it to keep %q", st.Detail, tc.want)
			}
		})
	}
}

func TestSystemdStatusLingerFallsBackToLogname(t *testing.T) {
	s, _ := newStatusSystemd(t, ScopeUser, show("loaded", "active", "success", "0", "enabled"), nil, false)
	env := s.lookupEnv
	s.lookupEnv = func(k string) (string, bool) {
		switch k {
		case "USER":
			return "", false
		case "LOGNAME":
			return "bob", true
		}
		return env(k)
	}
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(st.Detail, "loginctl enable-linger bob") {
		t.Fatalf("Detail = %q, want the linger note for bob", st.Detail)
	}

	s.lookupEnv = func(k string) (string, bool) {
		if k == "USER" || k == "LOGNAME" {
			return "", false
		}
		return env(k)
	}
	if st, err = s.Status(context.Background()); err != nil || st.Detail != "" {
		t.Fatalf("Status = %+v, %v, want no detail without a user name", st, err)
	}
}

func TestSystemdStatusEnabled(t *testing.T) {
	tests := []struct {
		fileState string
		want      bool
	}{
		{"enabled", true},
		{"enabled-runtime", true},
		{"disabled", false},
		{"static", false},
	}
	for _, tc := range tests {
		t.Run(tc.fileState, func(t *testing.T) {
			s, _ := newStatusSystemd(t, ScopeSystem, show("loaded", "active", "success", "0", tc.fileState), nil, false)
			st, err := s.Status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st.Enabled != tc.want {
				t.Fatalf("Enabled = %v, want %v", st.Enabled, tc.want)
			}
		})
	}
}

func TestSystemdStatusNeedsNoRoot(t *testing.T) {
	s, _ := newStatusSystemd(t, ScopeSystem, show("loaded", "active", "success", "0", "enabled"), nil, false)
	s.euid = 1000
	if _, err := s.Status(context.Background()); err != nil {
		t.Fatalf("Status as a non-root user: %v", err)
	}
}

func TestSystemdStatusUnreachableSystemManager(t *testing.T) {
	busErr := errors.New("systemctl show: exit status 1: Failed to connect to bus: No such file or directory")
	s, _ := newStatusSystemd(t, ScopeSystem, "", busErr, false)
	st, err := s.Status(context.Background())
	if !errors.Is(err, ErrManagerUnreachable) {
		t.Fatalf("Status error = %v, want ErrManagerUnreachable", err)
	}
	want := Status{State: StateUnknown, Detail: "the system service manager is unreachable"}
	if st != want {
		t.Fatalf("Status = %+v, want %+v", st, want)
	}
}

func TestSystemdStatusUnreadableMainPID(t *testing.T) {
	out := "LoadState=loaded\nActiveState=active\nResult=success\nMainPID=forty-two\nNRestarts=0\nUnitFileState=enabled\n"
	s, _ := newStatusSystemd(t, ScopeSystem, out, nil, false)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateUnknown || st.PID != 0 || !st.Enabled {
		t.Fatalf("Status = %+v, want unknown, enabled, with no PID", st)
	}
	if want := `systemd reports an unreadable MainPID="forty-two" for demo.service`; st.Detail != want {
		t.Fatalf("Detail = %q, want %q", st.Detail, want)
	}
}

func TestJoinDetail(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"", "", ""},
		{"", "b", "b"},
		{"a", "", "a"},
		{"a", "b", "a; b"},
	}
	for _, tc := range tests {
		if got := joinDetail(tc.a, tc.b); got != tc.want {
			t.Errorf("joinDetail(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

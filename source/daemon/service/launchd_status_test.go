package service

import (
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

// printAnswer is what a fake launchctl command prints and how it ends.
type printAnswer struct {
	out string
	err error
}

// printRunner records every argv it is given and answers a command whose
// argv, joined by spaces, is a key of answers; any other command prints
// nothing and succeeds.
type printRunner struct {
	calls   [][]string
	answers map[string]printAnswer
}

func (r *printRunner) run(_ context.Context, argv []string) ([]byte, error) {
	r.calls = append(r.calls, slices.Clone(argv))
	a := r.answers[strings.Join(argv, " ")]
	return []byte(a.out), a.err
}

// launchdFixture returns the content of testdata/launchd/<name>.
func launchdFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "launchd", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// newStatusLaunchd returns an installed launchd job "com.example.demo" of
// scope, the label the print fixtures name, whose `launchctl print` of the
// job gives answer and whose print-disabled answers with
// print-disabled.txt, where the job is enabled.
func newStatusLaunchd(t *testing.T, scope Scope, answer printAnswer) (*testLaunchd, *printRunner) {
	t.Helper()
	tl := newTestLaunchd(t, scope, 501)
	tl.cfg.Name = "com.example.demo"
	tl.writePlist(t)
	r := &printRunner{answers: map[string]printAnswer{
		"launchctl print " + tl.target():          answer,
		"launchctl print-disabled " + tl.domain(): {out: launchdFixture(t, "print-disabled.txt")},
	}}
	tl.run = r.run
	return tl, r
}

func TestLaunchdStatusMapsPrintOutput(t *testing.T) {
	tests := []struct {
		name     string
		print    printAnswer
		want     State
		pid      int
		restarts int
		detail   string
	}{
		{"not loaded", printAnswer{err: notFound()}, StateStopped, 0, 0, ""},
		{"not loaded, exit 3", printAnswer{err: noSuchProcess()}, StateStopped, 0, 0, ""},
		{"running", printAnswer{out: "print-running.txt"}, StateRunning, 4242, 0, ""},
		{"running after earlier crashes", printAnswer{out: "print-running-after-crashes.txt"}, StateRunning, 4242, 3, ""},
		{"running after a clean exit", printAnswer{out: "print-running-after-clean-exit.txt"}, StateRunning, 4242, 0, ""},
		{"crash loop", printAnswer{out: "print-crashed.txt"}, StateFailed, 0, 2, "last exit code = 78: EX_CONFIG"},
		{"first run crashed", printAnswer{out: "print-first-run-crashed.txt"}, StateStarting, 0, 0, ""},
		{"killed by a signal", printAnswer{out: "print-killed.txt"}, StateFailed, 0, 1, "last terminating signal = Killed: 9"},
		{"clean exit", printAnswer{out: "print-clean-exit.txt"}, StateStarting, 0, 0, ""},
		{"never exited", printAnswer{out: "print-never-exited.txt"}, StateStarting, 0, 0, ""},
		{"nested state and pid", printAnswer{out: "print-nested-state.txt"}, StateStarting, 0, 0, ""},
		{"unrecognised", printAnswer{out: "print-unrecognised.txt"}, StateUnknown, 0, 0, "reports no state"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			answer := tc.print
			if answer.out != "" {
				answer.out = launchdFixture(t, answer.out)
			}
			tl, _ := newStatusLaunchd(t, ScopeUser, answer)
			st, err := tl.Status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st.State != tc.want {
				t.Fatalf("State = %v, want %v (detail %q)", st.State, tc.want, st.Detail)
			}
			if st.PID != tc.pid || st.Restarts != tc.restarts {
				t.Fatalf("PID, Restarts = %d, %d, want %d, %d", st.PID, st.Restarts, tc.pid, tc.restarts)
			}
			if tc.detail == "" && st.Detail != "" {
				t.Fatalf("Detail = %q, want none", st.Detail)
			}
			if !strings.Contains(st.Detail, tc.detail) {
				t.Fatalf("Detail = %q, want it to mention %q", st.Detail, tc.detail)
			}
			if !st.Enabled {
				t.Fatal("Enabled = false, want true")
			}
		})
	}
}

func TestLaunchdStatusUnreadableValues(t *testing.T) {
	running := "gui/501/com.example.demo = {\n\tstate = running\n\tpid = 4242\n\truns = 1\n}\n"
	tests := []struct {
		name, out, detail string
	}{
		{"pid", strings.Replace(running, "pid = 4242", "pid = many", 1), "pid"},
		{"runs", strings.Replace(running, "runs = 1", "runs = -1", 1), "runs"},
		{"last exit code", strings.Replace(running, "runs = 1", "last exit code = crashed", 1), "last exit code"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl, _ := newStatusLaunchd(t, ScopeUser, printAnswer{out: tc.out})
			st, err := tl.Status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st.State != StateUnknown || !strings.Contains(st.Detail, tc.detail) {
				t.Fatalf("Status = %+v, want unknown mentioning %q", st, tc.detail)
			}
		})
	}
}

func TestLaunchdStatusFailureNamesTheLogFiles(t *testing.T) {
	crashed := printAnswer{out: launchdFixture(t, "print-crashed.txt")}

	daemon, _ := newStatusLaunchd(t, ScopeSystem, crashed)
	st, err := daemon.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	log := daemon.logs.stdout
	if st.State != StateFailed || !strings.HasSuffix(st.Detail, "see "+log) {
		t.Fatalf("Status = %+v, want failed pointing to %s alone", st, log)
	}

	agent, _ := newStatusLaunchd(t, ScopeUser, crashed)
	agent.logs = launchdLogs{stdout: "/tmp/demo.out", stderr: "/tmp/demo.err"}
	st, err = agent.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateFailed || !strings.HasSuffix(st.Detail, "see /tmp/demo.out and /tmp/demo.err") {
		t.Fatalf("Status = %+v, want failed pointing to both log files", st)
	}
}

func TestLaunchdStatusCommands(t *testing.T) {
	tests := []struct {
		scope Scope
		want  [][]string
	}{
		{ScopeSystem, [][]string{
			{"launchctl", "print", "system/com.example.demo"},
			{"launchctl", "print-disabled", "system"},
		}},
		{ScopeUser, [][]string{
			{"launchctl", "print", "gui/501"},
			{"launchctl", "print", "gui/501/com.example.demo"},
			{"launchctl", "print-disabled", "gui/501"},
		}},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.scope), func(t *testing.T) {
			tl, r := newStatusLaunchd(t, tc.scope, printAnswer{out: launchdFixture(t, "print-running.txt")})
			// A LaunchDaemon's Status needs no root.
			tl.euid = 501
			if _, err := tl.Status(context.Background()); err != nil {
				t.Fatalf("Status: %v", err)
			}
			if !slices.EqualFunc(r.calls, tc.want, slices.Equal[[]string]) {
				t.Fatalf("commands = %q, want %q", r.calls, tc.want)
			}
		})
	}
}

func TestLaunchdStatusNotInstalledRunsNoCommand(t *testing.T) {
	for _, scope := range []Scope{ScopeSystem, ScopeUser} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			tl := newTestLaunchd(t, scope, 501)
			st, err := tl.Status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st != (Status{State: StateNotInstalled}) {
				t.Fatalf("Status = %+v, want only StateNotInstalled", st)
			}
			if len(tl.runner.calls) != 0 {
				t.Fatalf("commands = %q, want none", tl.runner.calls)
			}
		})
	}
}

func TestLaunchdStatusMissingGUIDomainIsUnreachable(t *testing.T) {
	tl, r := newStatusLaunchd(t, ScopeUser, printAnswer{out: launchdFixture(t, "print-running.txt")})
	r.answers["launchctl print gui/501"] = printAnswer{err: notFound()}
	st, err := tl.Status(context.Background())
	if !errors.Is(err, ErrManagerUnreachable) {
		t.Fatalf("Status error = %v, want ErrManagerUnreachable", err)
	}
	if st.State != StateUnknown || !strings.Contains(st.Detail, "gui/501 is unavailable") ||
		!strings.Contains(st.Detail, "logged in to the GUI") {
		t.Fatalf("Status = %+v, want unknown explaining the missing gui domain", st)
	}
	if !slices.EqualFunc(r.calls, [][]string{{"launchctl", "print", "gui/501"}}, slices.Equal[[]string]) {
		t.Fatalf("commands = %q, want only the domain check", r.calls)
	}
}

func TestLaunchdStatusMissingLaunchctlIsUnsupported(t *testing.T) {
	for _, scope := range []Scope{ScopeSystem, ScopeUser} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			tl, _ := newStatusLaunchd(t, scope, printAnswer{})
			missing := fmt.Errorf("launchctl: %w", exec.ErrNotFound)
			tl.run = func(context.Context, []string) ([]byte, error) { return nil, missing }
			st, err := tl.Status(context.Background())
			if !errors.Is(err, ErrUnsupported) || errors.Is(err, ErrManagerUnreachable) {
				t.Fatalf("Status error = %v, want ErrUnsupported alone", err)
			}
			if st.State != StateUnknown {
				t.Fatalf("State = %v, want unknown", st.State)
			}
		})
	}
}

func TestLaunchdStatusPassesOtherPrintErrorsThrough(t *testing.T) {
	boom := exitStatusError{code: 1}
	tl, r := newStatusLaunchd(t, ScopeSystem, printAnswer{err: boom})
	st, err := tl.Status(context.Background())
	if !errors.Is(err, boom) || errors.Is(err, ErrManagerUnreachable) {
		t.Fatalf("Status error = %v, want the exit status alone", err)
	}
	if st.State != StateUnknown {
		t.Fatalf("State = %v, want unknown", st.State)
	}
	if len(r.calls) != 1 {
		t.Fatalf("commands = %q, want no print-disabled", r.calls)
	}
}

func TestLaunchdStatusPrintDisabledFailureKeepsTheState(t *testing.T) {
	tl, r := newStatusLaunchd(t, ScopeUser, printAnswer{out: launchdFixture(t, "print-running.txt")})
	boom := exitStatusError{code: 1}
	r.answers["launchctl print-disabled gui/501"] = printAnswer{err: boom}
	st, err := tl.Status(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Status error = %v, want the print-disabled failure", err)
	}
	if st.State != StateRunning || st.PID != 4242 || st.Enabled ||
		!strings.Contains(st.Detail, "whether com.example.demo is enabled is unknown") {
		t.Fatalf("Status = %+v, want running, not enabled, with a Detail on the enabled state", st)
	}
}

func TestLaunchdStatusReadsEnabledFromPrintDisabled(t *testing.T) {
	tl, r := newStatusLaunchd(t, ScopeUser, printAnswer{out: launchdFixture(t, "print-running.txt")})
	r.answers["launchctl print-disabled gui/501"] = printAnswer{out: launchdFixture(t, "print-disabled-bool.txt")}
	tl.cfg.Name = "com.example.off"
	r.answers["launchctl print gui/501/com.example.off"] = r.answers["launchctl print gui/501/com.example.demo"]
	tl.writePlist(t)
	st, err := tl.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateRunning || st.Enabled {
		t.Fatalf("Status = %+v, want running and disabled", st)
	}
}

func TestLaunchdDisabled(t *testing.T) {
	tests := []struct {
		fixture, label string
		want           bool
	}{
		{"print-disabled.txt", "com.example.demo", false},
		{"print-disabled.txt", "com.example.off", true},
		{"print-disabled.txt", "com.example.absent", false},
		{"print-disabled-bool.txt", "com.example.demo", false},
		{"print-disabled-bool.txt", "com.example.off", true},
		{"print-disabled-bool.txt", "com.example.absent", false},
	}
	for _, tc := range tests {
		t.Run(tc.fixture+"/"+tc.label, func(t *testing.T) {
			if got := launchdDisabled([]byte(launchdFixture(t, tc.fixture)), tc.label); got != tc.want {
				t.Fatalf("launchdDisabled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLaunchdDisabledSkipsOtherBlocks(t *testing.T) {
	out := "login item associations = {\n\t\"com.example.demo\" => disabled\n}\n" +
		"disabled services = {\n\t\"com.example.other\" => disabled\n}\n"
	if launchdDisabled([]byte(out), "com.example.demo") {
		t.Fatal("launchdDisabled = true, want a label outside disabled services ignored")
	}
}

func TestParseLaunchdPrintReadsOnlyTheServiceBlock(t *testing.T) {
	props := parseLaunchdPrint([]byte(launchdFixture(t, "print-nested-state.txt")))
	if props["state"] != "not running" {
		t.Fatalf("state = %q, want not running", props["state"])
	}
	if _, ok := props["pid"]; ok {
		t.Fatalf("pid = %q, want none from a nested block", props["pid"])
	}
	if props["port"] != "" || props["keepalive"] != "" {
		t.Fatalf("props = %q, want no nested key", props)
	}

	bare := parseLaunchdPrint([]byte("state = running\npid = 7\n"))
	if bare["state"] != "running" || bare["pid"] != "7" {
		t.Fatalf("props = %q, want the top level read when there is no block", bare)
	}
}

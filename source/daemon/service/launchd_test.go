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
	"time"
)

// exitStatusError is a command failure carrying an exit status, as the
// *exec.ExitError that execRunner wraps does.
type exitStatusError struct{ code int }

func (e exitStatusError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e exitStatusError) ExitCode() int { return e.code }

// scriptedRunner records every argv it is given. A command whose argv, joined
// by spaces, is a key of script gets the next error of its list, and the last
// one once the list is spent; a nil entry is a success. Any other command
// succeeds.
type scriptedRunner struct {
	calls  [][]string
	script map[string][]error
}

func (r *scriptedRunner) run(_ context.Context, argv []string) ([]byte, error) {
	r.calls = append(r.calls, slices.Clone(argv))
	key := strings.Join(argv, " ")
	errs := r.script[key]
	if len(errs) == 0 {
		return nil, nil
	}
	err := errs[0]
	if len(errs) > 1 {
		r.script[key] = errs[1:]
	}
	return nil, err
}

// on scripts the results of the command argv.
func (r *scriptedRunner) on(argv string, errs ...error) { r.script[argv] = errs }

// testLaunchd is a launchd driver for the job "demo" whose files are under a
// temporary directory, with its fake runner and the sleeps it asked for.
type testLaunchd struct {
	*launchd
	runner  *scriptedRunner
	slept   []time.Duration
	homeDir string
}

// newTestLaunchd returns a launchd driver for the job "demo" run by uid 501.
// A LaunchAgent's HOME is a temporary directory; a LaunchDaemon's plist
// directory and log files are under a temporary directory, so no test writes
// to /Library.
func newTestLaunchd(t *testing.T, scope Scope, euid int) *testLaunchd {
	t.Helper()
	root := t.TempDir()
	cfg := Config{Name: "demo", Description: "Demo", Binary: "/usr/local/bin/demo", Scope: scope}
	tl := &testLaunchd{runner: &scriptedRunner{script: map[string][]error{}}, homeDir: filepath.Join(root, "home")}
	tl.launchd = &launchd{
		cfg:  cfg,
		run:  tl.runner.run,
		euid: euid,
		uid:  501,
		lookupEnv: func(k string) (string, bool) {
			if k == "HOME" {
				return tl.homeDir, true
			}
			return "", false
		},
		sleep: func(_ context.Context, d time.Duration) error {
			tl.slept = append(tl.slept, d)
			return nil
		},
		daemonDir: filepath.Join(root, "Library", "LaunchDaemons"),
	}
	if scope == ScopeSystem {
		cfg.User = "demo"
		tl.cfg = cfg
		log := filepath.Join(root, "Library", "Logs", "demo", "demo.log")
		tl.logs = launchdLogs{stdout: log, stderr: log}
	}
	return tl
}

// plist returns the path of the job's property list.
func (tl *testLaunchd) plist(t *testing.T) string {
	t.Helper()
	p, err := tl.paths()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	return p.plist
}

// writePlist installs a placeholder property list and returns its path.
func (tl *testLaunchd) writePlist(t *testing.T) string {
	t.Helper()
	path := tl.plist(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<plist/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (tl *testLaunchd) wantCalls(t *testing.T, want [][]string) {
	t.Helper()
	if !slices.EqualFunc(tl.runner.calls, want, slices.Equal[[]string]) {
		t.Fatalf("commands = %q, want %q", tl.runner.calls, want)
	}
}

func notFound() error      { return exitStatusError{code: launchctlNotFound} }
func noSuchProcess() error { return exitStatusError{code: launchctlNoSuchProcess} }

func TestLaunchdFreshInstall(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		euid  int
		want  func(plist string) [][]string
	}{
		{"agent", ScopeUser, 501, func(plist string) [][]string {
			return [][]string{
				{"launchctl", "print", "gui/501"},
				{"launchctl", "enable", "gui/501/demo"},
				{"launchctl", "bootout", "gui/501/demo"},
				{"launchctl", "print", "gui/501/demo"},
				{"launchctl", "bootstrap", "gui/501", plist},
			}
		}},
		{"daemon", ScopeSystem, 0, func(plist string) [][]string {
			return [][]string{
				{"id", "-u", "--", "demo"},
				{"launchctl", "enable", "system/demo"},
				{"launchctl", "bootout", "system/demo"},
				{"launchctl", "print", "system/demo"},
				{"launchctl", "bootstrap", "system", plist},
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestLaunchd(t, tc.scope, tc.euid)
			tl.runner.on("launchctl bootout "+tl.target(), notFound())
			tl.runner.on("launchctl print "+tl.target(), notFound())
			if err := tl.Install(context.Background()); err != nil {
				t.Fatalf("Install: %v", err)
			}
			tl.wantCalls(t, tc.want(tl.plist(t)))
			if len(tl.slept) != 0 {
				t.Fatalf("slept %v, want no wait for a job that is not loaded", tl.slept)
			}
		})
	}
}

func TestLaunchdInstallPlacesThePlistByScope(t *testing.T) {
	agent := newTestLaunchd(t, ScopeUser, 501)
	if got, want := agent.plist(t), filepath.Join(agent.homeDir, "Library", "LaunchAgents", "demo.plist"); got != want {
		t.Fatalf("agent plist = %q, want %q", got, want)
	}
	daemon := newTestLaunchd(t, ScopeSystem, 0)
	daemon.daemonDir = ""
	if got, want := daemon.plist(t), "/Library/LaunchDaemons/demo.plist"; got != want {
		t.Fatalf("daemon plist = %q, want %q", got, want)
	}
}

func TestLaunchdInstallOverALoadedJobWaitsForTheBootout(t *testing.T) {
	for name, gone := range map[string]error{"exit 113": notFound(), "exit 3": noSuchProcess()} {
		t.Run(name, func(t *testing.T) { installOverALoadedJob(t, gone) })
	}
}

func installOverALoadedJob(t *testing.T, gone error) {
	t.Helper()
	tl := newTestLaunchd(t, ScopeUser, 501)
	tl.runner.on("launchctl print gui/501/demo", nil, nil, gone)
	if err := tl.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tl.wantCalls(t, [][]string{
		{"launchctl", "print", "gui/501"},
		{"launchctl", "enable", "gui/501/demo"},
		{"launchctl", "bootout", "gui/501/demo"},
		{"launchctl", "print", "gui/501/demo"},
		{"launchctl", "print", "gui/501/demo"},
		{"launchctl", "print", "gui/501/demo"},
		{"launchctl", "bootstrap", "gui/501", tl.plist(t)},
	})
	if want := []time.Duration{launchdPollInterval, launchdPollInterval}; !slices.Equal(tl.slept, want) {
		t.Fatalf("slept %v, want %v", tl.slept, want)
	}
}

func TestLaunchdInstallGivesUpWaitingAfterTheTimeout(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	err := tl.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "still loaded") || !strings.Contains(err.Error(), tl.plist(t)) {
		t.Fatalf("err = %v, want the job still loaded with the plist installed", err)
	}
	var total time.Duration
	for _, d := range tl.slept {
		total += d
	}
	if total != launchdUnloadTimeout {
		t.Fatalf("slept %v in total, want %v", total, launchdUnloadTimeout)
	}
	for _, argv := range tl.runner.calls {
		if argv[1] == "bootstrap" {
			t.Fatalf("commands = %q, want no bootstrap", tl.runner.calls)
		}
	}
	if _, err := os.Stat(tl.plist(t)); err != nil {
		t.Fatalf("plist stat err = %v, want it installed", err)
	}
}

func TestLaunchdInstallStopsWaitingWhenSleepFails(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	tl.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	if err := tl.Install(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestLaunchdInstallBootstrapFailureLeavesThePlistInstalled(t *testing.T) {
	for _, code := range []int{launchctlIOError, 1} {
		t.Run(fmt.Sprint("exit ", code), func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeUser, 501)
			tl.runner.on("launchctl print gui/501/demo", notFound())
			boom := exitStatusError{code: code}
			tl.runner.on("launchctl bootstrap gui/501 "+tl.plist(t), boom)
			err := tl.Install(context.Background())
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), "installed at "+tl.plist(t)+" but not loaded") ||
				!strings.Contains(err.Error(), "Start retries") {
				t.Fatalf("err = %v, want the plist installed but not loaded", err)
			}
			// Only EIO names the race with an unfinished bootout.
			if got, want := strings.Contains(err.Error(), "launchd may still be unloading the job"), code == launchctlIOError; got != want {
				t.Fatalf("err = %v, mentions an unfinished unload = %v, want %v", err, got, want)
			}
			if _, err := os.Stat(tl.plist(t)); err != nil {
				t.Fatalf("plist stat err = %v, want it kept", err)
			}
		})
	}
}

func TestLaunchdInstallStopsAtAFailingStep(t *testing.T) {
	steps := []string{"launchctl enable gui/501/demo", "launchctl bootout gui/501/demo", "launchctl print gui/501/demo"}
	for i, step := range steps {
		t.Run(step, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeUser, 501)
			boom := exitStatusError{code: 1}
			tl.runner.on(step, boom)
			if err := tl.Install(context.Background()); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the %s failure", err, step)
			}
			// The domain check runs first, then every step up to the failing one.
			if len(tl.runner.calls) != i+2 {
				t.Fatalf("commands = %q, want to stop after %s", tl.runner.calls, step)
			}
		})
	}
}

func TestLaunchdInstallWritesTheRenderedPlist(t *testing.T) {
	for _, scope := range []Scope{ScopeSystem, ScopeUser} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			tl := newTestLaunchd(t, scope, 0)
			tl.runner.on("launchctl print "+tl.target(), notFound())
			if err := tl.Install(context.Background()); err != nil {
				t.Fatalf("Install: %v", err)
			}
			path := tl.plist(t)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			p, err := tl.paths()
			if err != nil {
				t.Fatal(err)
			}
			if want := renderPlist(tl.cfg, p.logs); !bytes.Equal(got, want) {
				t.Fatalf("plist =\n%s\nwant\n%s", got, want)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o644 {
				t.Fatalf("plist mode = %v, want 0644", fi.Mode().Perm())
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("plist directory holds %d entries, want only the plist", len(entries))
			}
		})
	}
}

func TestLaunchdLogPathsTakeTheOptionOverTheDefault(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	tl.logs = launchdLogs{stderr: "/var/log/demo.err"}
	p, err := tl.paths()
	if err != nil {
		t.Fatal(err)
	}
	got := p.logs
	want := launchdLogs{
		stdout: filepath.Join(tl.homeDir, "Library", "Logs", "demo", "demo.log"),
		stderr: "/var/log/demo.err",
	}
	if got != want {
		t.Fatalf("logs = %+v, want %+v", got, want)
	}
}

func TestLaunchdPrepareCreatesThePlistAndLogDirectories(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	out := filepath.Join(t.TempDir(), "out", "demo.log")
	tl.logs = launchdLogs{stdout: out}
	if err := tl.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for _, dir := range []string{
		filepath.Dir(tl.plist(t)),
		filepath.Dir(out),
		filepath.Join(tl.homeDir, "Library", "Logs", "demo"),
	} {
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			t.Fatalf("stat %s = %v, want a directory", dir, err)
		}
		if fi.Mode().Perm() != 0o750 {
			t.Fatalf("%s mode = %v, want 0750", dir, fi.Mode().Perm())
		}
	}
	if len(tl.runner.calls) != 0 {
		t.Fatalf("commands = %q, want none", tl.runner.calls)
	}
}

func TestLaunchdPrepareFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, tl *testLaunchd)
		want  string
	}{
		{"plist directory", func(t *testing.T, tl *testLaunchd) {
			blocker := filepath.Join(t.TempDir(), "home")
			if err := os.WriteFile(blocker, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			tl.homeDir = blocker
		}, "create plist directory"},
		{"log directory", func(t *testing.T, tl *testLaunchd) {
			blocker := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(blocker, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			tl.logs = launchdLogs{stdout: filepath.Join(blocker, "demo.log")}
		}, "create log directory"},
	}
	methods := map[string]func(*launchd, context.Context) error{
		"Prepare": (*launchd).Prepare,
		"Install": (*launchd).Install,
	}
	for _, tc := range tests {
		for name, method := range methods {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				tl := newTestLaunchd(t, ScopeUser, 501)
				tc.setup(t, tl)
				if err := method(tl.launchd, context.Background()); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err = %v, want %q", err, tc.want)
				}
				for _, argv := range tl.runner.calls {
					if argv[1] != "print" {
						t.Fatalf("commands = %q, want none past the domain check", tl.runner.calls)
					}
				}
			})
		}
	}
}

func TestLaunchdDaemonNeedsRoot(t *testing.T) {
	methods := map[string]func(*launchd, context.Context) error{
		"Prepare":   (*launchd).Prepare,
		"Install":   (*launchd).Install,
		"Uninstall": (*launchd).Uninstall,
		"Start":     (*launchd).Start,
		"Stop":      (*launchd).Stop,
	}
	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeSystem, 501)
			err := method(tl.launchd, context.Background())
			if !errors.Is(err, ErrPrivilegesRequired) || !strings.Contains(err.Error(), "sudo") {
				t.Fatalf("err = %v, want ErrPrivilegesRequired suggesting sudo", err)
			}
			if len(tl.runner.calls) != 0 {
				t.Fatalf("commands = %q, want none", tl.runner.calls)
			}
			if _, err := os.Stat(tl.daemonDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("plist directory stat err = %v, want it absent", err)
			}
		})
	}
}

func TestLaunchdInstallMissingAccount(t *testing.T) {
	tl := newTestLaunchd(t, ScopeSystem, 0)
	tl.runner.on("id -u -- demo", errors.New("id -u -- demo: exit status 1: id: demo: no such user"))
	err := tl.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), `"demo"`) {
		t.Fatalf("err = %v, want an error naming the account", err)
	}
	tl.wantCalls(t, [][]string{{"id", "-u", "--", "demo"}})
	if _, err := os.Stat(tl.daemonDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plist directory stat err = %v, want it absent", err)
	}
	if _, err := os.Stat(filepath.Dir(tl.logs.stdout)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("log directory stat err = %v, want it absent", err)
	}
}

func TestLaunchdMissingGUIDomainIsUnreachable(t *testing.T) {
	methods := map[string]func(*launchd, context.Context) error{
		"Install":   (*launchd).Install,
		"Uninstall": (*launchd).Uninstall,
		"Start":     (*launchd).Start,
		"Stop":      (*launchd).Stop,
	}
	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeUser, 501)
			var plist string
			if name != "Install" {
				plist = tl.writePlist(t)
			}
			tl.runner.on("launchctl print gui/501", exitStatusError{code: launchctlNotFound})
			err := method(tl.launchd, context.Background())
			if !errors.Is(err, ErrManagerUnreachable) || !strings.Contains(err.Error(), "logged in to the GUI") {
				t.Fatalf("err = %v, want ErrManagerUnreachable asking for a GUI login", err)
			}
			tl.wantCalls(t, [][]string{{"launchctl", "print", "gui/501"}})
			if name == "Install" {
				if _, err := os.Stat(filepath.Join(tl.homeDir, "Library")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("stat err = %v, want nothing written", err)
				}
			} else if _, err := os.Stat(plist); err != nil {
				t.Fatalf("plist stat err = %v, want it kept", err)
			}
		})
	}
}

func TestLaunchdMissingLaunchctlIsUnsupported(t *testing.T) {
	for _, scope := range []Scope{ScopeSystem, ScopeUser} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			tl := newTestLaunchd(t, scope, 0)
			missing := fmt.Errorf("launchctl: %w", exec.ErrNotFound)
			tl.run = func(ctx context.Context, argv []string) ([]byte, error) {
				if argv[0] == "launchctl" {
					return nil, missing
				}
				return tl.runner.run(ctx, argv)
			}
			err := tl.Install(context.Background())
			if !errors.Is(err, ErrUnsupported) || errors.Is(err, ErrManagerUnreachable) {
				t.Fatalf("err = %v, want ErrUnsupported alone", err)
			}
		})
	}
}

func TestLaunchdHomeUnsetFailsAnAgent(t *testing.T) {
	methods := map[string]func(*launchd, context.Context) error{
		"Prepare":   (*launchd).Prepare,
		"Install":   (*launchd).Install,
		"Uninstall": (*launchd).Uninstall,
		"Start":     (*launchd).Start,
		"Stop":      (*launchd).Stop,
	}
	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeUser, 501)
			tl.lookupEnv = func(string) (string, bool) { return "", false }
			err := method(tl.launchd, context.Background())
			if err == nil || !strings.Contains(err.Error(), "HOME is not set") {
				t.Fatalf("err = %v, want HOME unset", err)
			}
		})
	}
	for name, lookup := range map[string]func(string) (string, bool){
		"unset": func(string) (string, bool) { return "", false },
		"empty": func(string) (string, bool) { return "", true },
	} {
		t.Run("paths/"+name, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeUser, 501)
			tl.lookupEnv = lookup
			if _, err := tl.paths(); err == nil || !strings.Contains(err.Error(), "HOME is not set") {
				t.Fatalf("paths err = %v, want HOME unset", err)
			}
		})
	}
}

func TestLaunchdNotInstalled(t *testing.T) {
	methods := map[string]func(*launchd, context.Context) error{
		"Uninstall": (*launchd).Uninstall,
		"Start":     (*launchd).Start,
		"Stop":      (*launchd).Stop,
	}
	for name, method := range methods {
		for _, scope := range []Scope{ScopeSystem, ScopeUser} {
			t.Run(fmt.Sprint(name, "/", scope), func(t *testing.T) {
				tl := newTestLaunchd(t, scope, 0)
				if err := method(tl.launchd, context.Background()); !errors.Is(err, ErrNotInstalled) {
					t.Fatalf("err = %v, want ErrNotInstalled", err)
				}
				if len(tl.runner.calls) != 0 {
					t.Fatalf("commands = %q, want none", tl.runner.calls)
				}
			})
		}
	}
}

func TestLaunchdUnreadablePlistDirIsNotNotInstalled(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	blocker := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tl.homeDir = blocker
	err := tl.Start(context.Background())
	if err == nil || errors.Is(err, ErrNotInstalled) || !strings.Contains(err.Error(), "stat plist") {
		t.Fatalf("err = %v, want a stat failure other than ErrNotInstalled", err)
	}
}

func TestLaunchdStart(t *testing.T) {
	t.Run("loaded", func(t *testing.T) {
		tl := newTestLaunchd(t, ScopeUser, 501)
		tl.writePlist(t)
		if err := tl.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		tl.wantCalls(t, [][]string{
			{"launchctl", "print", "gui/501"},
			{"launchctl", "print", "gui/501/demo"},
			{"launchctl", "kickstart", "gui/501/demo"},
		})
	})
	for name, gone := range map[string]error{"not loaded": notFound(), "not loaded, exit 3": noSuchProcess()} {
		t.Run(name, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeSystem, 0)
			plist := tl.writePlist(t)
			tl.runner.on("launchctl print system/demo", gone)
			if err := tl.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			tl.wantCalls(t, [][]string{
				{"launchctl", "print", "system/demo"},
				{"launchctl", "bootstrap", "system", plist},
			})
		})
	}
}

func TestLaunchdStartPassesLaunchctlErrorsThrough(t *testing.T) {
	steps := map[string]error{
		"launchctl print system/demo":     exitStatusError{code: 1},
		"launchctl kickstart system/demo": exitStatusError{code: 1},
	}
	for step, boom := range steps {
		t.Run(step, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeSystem, 0)
			tl.writePlist(t)
			tl.runner.on(step, boom)
			if err := tl.Start(context.Background()); !errors.Is(err, boom) || !strings.Contains(err.Error(), "start demo") {
				t.Fatalf("err = %v, want the %s failure", err, step)
			}
		})
	}
	t.Run("bootstrap", func(t *testing.T) {
		tl := newTestLaunchd(t, ScopeSystem, 0)
		plist := tl.writePlist(t)
		boom := exitStatusError{code: 1}
		tl.runner.on("launchctl print system/demo", notFound())
		tl.runner.on("launchctl bootstrap system "+plist, boom)
		if err := tl.Start(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the bootstrap failure", err)
		}
	})
}

func TestLaunchdStop(t *testing.T) {
	tests := []struct {
		name    string
		bootout error
	}{
		{"loaded", nil},
		{"not loaded, exit 3", noSuchProcess()},
		{"not loaded, exit 113", notFound()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeUser, 501)
			plist := tl.writePlist(t)
			tl.runner.on("launchctl bootout gui/501/demo", tc.bootout)
			tl.runner.on("launchctl print gui/501/demo", notFound())
			if err := tl.Stop(context.Background()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			tl.wantCalls(t, [][]string{
				{"launchctl", "print", "gui/501"},
				{"launchctl", "bootout", "gui/501/demo"},
				{"launchctl", "print", "gui/501/demo"},
			})
			if _, err := os.Stat(plist); err != nil {
				t.Fatalf("plist stat err = %v, want it kept", err)
			}
		})
	}
}

func TestLaunchdStopFailures(t *testing.T) {
	t.Run("bootout", func(t *testing.T) {
		tl := newTestLaunchd(t, ScopeSystem, 0)
		tl.writePlist(t)
		boom := exitStatusError{code: 1}
		tl.runner.on("launchctl bootout system/demo", boom)
		if err := tl.Stop(context.Background()); !errors.Is(err, boom) || !strings.Contains(err.Error(), "stop demo") {
			t.Fatalf("err = %v, want the bootout failure", err)
		}
		tl.wantCalls(t, [][]string{{"launchctl", "bootout", "system/demo"}})
	})
	t.Run("still loaded", func(t *testing.T) {
		tl := newTestLaunchd(t, ScopeSystem, 0)
		tl.writePlist(t)
		if err := tl.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "still loaded") {
			t.Fatalf("err = %v, want the job still loaded", err)
		}
	})
}

func TestLaunchdUninstall(t *testing.T) {
	bootoutErr := exitStatusError{code: 1}
	disableErr := exitStatusError{code: 2}
	tests := []struct {
		name             string
		bootout, disable error
		wantErr          []error
	}{
		{"both succeed", nil, nil, nil},
		{"not loaded", notFound(), nil, nil},
		{"bootout fails", bootoutErr, nil, []error{bootoutErr}},
		{"disable fails", nil, disableErr, []error{disableErr}},
		{"both fail", bootoutErr, disableErr, []error{bootoutErr, disableErr}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestLaunchd(t, ScopeSystem, 0)
			plist := tl.writePlist(t)
			tl.runner.on("launchctl bootout system/demo", tc.bootout)
			tl.runner.on("launchctl disable system/demo", tc.disable)
			err := tl.Uninstall(context.Background())
			if (err != nil) != (len(tc.wantErr) > 0) {
				t.Fatalf("err = %v, want an error exactly when bootout or disable fails", err)
			}
			for _, want := range tc.wantErr {
				if !errors.Is(err, want) || !strings.Contains(err.Error(), "plist removed") {
					t.Fatalf("err = %v, want it to carry %v and say the plist is removed", err, want)
				}
			}
			tl.wantCalls(t, [][]string{
				{"launchctl", "bootout", "system/demo"},
				{"launchctl", "disable", "system/demo"},
			})
			if _, err := os.Stat(plist); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("plist stat err = %v, want it removed", err)
			}
		})
	}
}

func TestLaunchdUninstallReportsAPlistThatCannotBeRemoved(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	plist := tl.plist(t)
	if err := os.MkdirAll(filepath.Join(plist, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	disableErr := exitStatusError{code: 2}
	tl.runner.on("launchctl disable gui/501/demo", disableErr)
	err := tl.Uninstall(context.Background())
	if err == nil || !strings.Contains(err.Error(), "remove plist") || !errors.Is(err, disableErr) {
		t.Fatalf("err = %v, want the removal and the disable failure", err)
	}
}

func TestLaunchdInstallStopsWhenThePlistCannotBeWritten(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	if err := os.MkdirAll(filepath.Join(tl.plist(t), "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tl.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "rename plist") {
		t.Fatalf("err = %v, want the plist rename to fail", err)
	}
	tl.wantCalls(t, [][]string{{"launchctl", "print", "gui/501"}})
}

func TestExitCodeReadsTheRunnersError(t *testing.T) {
	if code, ok := exitCode(fmt.Errorf("launchctl print: %w", notFound())); !ok || code != launchctlNotFound {
		t.Fatalf("exitCode = %d, %v, want %d, true", code, ok, launchctlNotFound)
	}
	if code, ok := exitCode(errors.New("boom")); ok || code != 0 {
		t.Fatalf("exitCode = %d, %v, want 0, false", code, ok)
	}
	_, err := execRunner(context.Background(), []string{"id", "-u", "--", "no-such-account-vipengele-test"})
	if code, ok := exitCode(err); !ok || code == 0 {
		t.Fatalf("exitCode(%v) = %d, %v, want a non-zero exit status", err, code, ok)
	}
}

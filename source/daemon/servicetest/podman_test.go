package servicetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/daemon/service"
)

// helperEnv makes the test binary act as a stand-in podman: it prints its
// arguments and its standard input, and when its first argument is "fail" it
// writes to standard error and exits 3.
const helperEnv = "SERVICETEST_HELPER_PODMAN"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(helperPodman(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func helperPodman(args []string) int {
	in, _ := io.ReadAll(os.Stdin)
	fmt.Printf("args=%s stdin=%s", strings.Join(args, ","), in)
	if len(args) > 0 && args[0] == "fail" {
		fmt.Fprint(os.Stderr, "boom")
		return 3
	}
	return 0
}

// call is one command a fakeRunner received.
type call struct {
	args  []string
	stdin string
}

// fakeRunner answers each command through handle and records it.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []call
	handle func(args []string) (string, error)
}

func (f *fakeRunner) run(_ context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	var in string
	if stdin != nil {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		in = string(b)
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{args: slices.Clone(args), stdin: in})
	f.mu.Unlock()
	if f.handle == nil {
		return nil, nil
	}
	out, err := f.handle(args)
	return []byte(out), err
}

// commands returns every recorded command, its arguments joined by spaces.
func (f *fakeRunner) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, strings.Join(c.args, " "))
	}
	return out
}

// noSleep is a harness sleep that returns at once while ctx is live.
func noSleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// countSleep returns a sleep that fails with context.DeadlineExceeded from
// call limit on, and the number of calls so far.
func countSleep(limit int) (func(context.Context, time.Duration) error, *int) {
	n := 0
	return func(context.Context, time.Duration) error {
		n++
		if n >= limit {
			return context.DeadlineExceeded
		}
		return nil
	}, &n
}

// fakeTB records what the code under test reports. Fatal and Skip end the
// goroutine runTB starts, as they do for a real test.
type fakeTB struct {
	testing.TB
	mu       sync.Mutex
	logs     []string
	errs     []string
	fatal    string
	skipped  bool
	cleanups []func()
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Logf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, fmt.Sprintf(format, args...))
}

func (f *fakeTB) Error(args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, fmt.Sprint(args...))
}

func (f *fakeTB) Fatal(args ...any) {
	f.mu.Lock()
	f.fatal = fmt.Sprint(args...)
	f.mu.Unlock()
	runtime.Goexit()
}

func (f *fakeTB) Skip(...any) {
	f.mu.Lock()
	f.skipped = true
	f.mu.Unlock()
	runtime.Goexit()
}

func (f *fakeTB) Cleanup(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, fn)
}

// runTB runs fn with a fakeTB on its own goroutine, then its cleanups in
// reverse order, and returns the fakeTB.
func runTB(t *testing.T, fn func(tb *fakeTB)) *fakeTB {
	t.Helper()
	f := &fakeTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
	for _, c := range slices.Backward(f.cleanups) {
		c()
	}
	return f
}

func (f *fakeTB) logged(sub string) bool {
	return slices.ContainsFunc(f.logs, func(l string) bool { return strings.Contains(l, sub) })
}

// testContainer is the container ID every fake run prints.
const testContainer = "c0ffee"

// execArgv returns the command a `podman exec` runs in the container, or nil
// for any other podman command.
func execArgv(args []string) []string {
	if len(args) == 0 || args[0] != "exec" {
		return nil
	}
	i := slices.Index(args, testContainer)
	if i < 0 {
		return nil
	}
	return args[i+1:]
}

func TestNewExecRunner(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"unset", nil, []string{"podman"}},
		{"blank", map[string]string{podmanEnv: "  "}, []string{"podman"}},
		{"sudo", map[string]string{podmanEnv: " sudo  -n podman "}, []string{"sudo", "-n", "podman"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }
			if got := newExecRunner(lookup).argv; !slices.Equal(got, tc.want) {
				t.Errorf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}

// helperArgv returns the test binary as a podman command line with the
// fixed argument prefix, after enabling the helper mode.
func helperArgv(t *testing.T, prefix ...string) []string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperEnv, "1")
	return append([]string{exe}, prefix...)
}

func TestExecRunnerRunsTheCommandLine(t *testing.T) {
	r := execRunner{argv: helperArgv(t, "pre")}
	out, err := r.run(t.Context(), strings.NewReader("input"), "version", "--x")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "args=pre,version,--x stdin=input"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestExecRunnerReportsStderrOnFailure(t *testing.T) {
	argv := helperArgv(t)
	r := execRunner{argv: argv}
	out, err := r.run(t.Context(), nil, "fail", "now")
	var ce *commandError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a *commandError", err)
	}
	if !strings.Contains(string(out), "args=fail,now") {
		t.Errorf("stdout = %q, want the helper's output", out)
	}
	msg := err.Error()
	if !strings.Contains(msg, argv[0]+" fail now: ") || !strings.HasSuffix(msg, ": boom") {
		t.Errorf("err = %q, want the command line and the stderr", msg)
	}
	var ee interface{ ExitCode() int }
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Errorf("err = %v, want it to unwrap to exit code 3", err)
	}
}

func TestCommandErrorWithoutStderr(t *testing.T) {
	err := &commandError{args: []string{"podman", "rm"}, stderr: " \n", err: errors.New("exit status 1")}
	if got, want := err.Error(), "podman rm: exit status 1"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewHarnessAcceptsAnAnsweringPodman(t *testing.T) {
	argv := helperArgv(t)
	if strings.ContainsAny(argv[0], " \t") {
		t.Skip("the test binary path holds whitespace, which SERVICETEST_PODMAN splits on")
	}
	lookup := func(string) (string, bool) { return argv[0], true }
	f := runTB(t, func(tb *fakeTB) { newHarness(tb, lookup) })
	if f.fatal != "" {
		t.Errorf("newHarness failed: %s", f.fatal)
	}
}

func TestNewHarnessFailsWhenPodmanDoesNotAnswer(t *testing.T) {
	argv := helperArgv(t)
	if strings.ContainsAny(argv[0], " \t") {
		t.Skip("the test binary path holds whitespace, which SERVICETEST_PODMAN splits on")
	}
	lookup := func(string) (string, bool) { return argv[0] + " fail", true }
	f := runTB(t, func(tb *fakeTB) { newHarness(tb, lookup) })
	if !strings.Contains(f.fatal, "podman is not usable") || !strings.Contains(f.fatal, podmanEnv) {
		t.Errorf("fatal = %q, want podman reported unusable", f.fatal)
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(t.Context(), time.Nanosecond); err != nil {
		t.Errorf("sleepContext = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepContext on a cancelled context = %v, want context.Canceled", err)
	}
}

func TestScopeName(t *testing.T) {
	for scope, want := range map[service.Scope]string{service.ScopeSystem: "system", service.ScopeUser: "user"} {
		if got, err := scopeName(scope); err != nil || got != want {
			t.Errorf("scopeName(%d) = %q, %v, want %q", scope, got, err, want)
		}
	}
	if _, err := scopeName(service.Scope(7)); err == nil {
		t.Error("scopeName(7) succeeded, want an error")
	}
}

func TestStartRunsAPrivilegedSystemdContainer(t *testing.T) {
	f := &fakeRunner{handle: func([]string) (string, error) { return testContainer + "\n", nil }}
	h := harness{r: f, sleep: noSleep}
	c, err := h.start(t.Context(), "img:tag", service.ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if c.id != testContainer || c.scope != service.ScopeUser {
		t.Errorf("container = %+v, want ID %s in the user scope", c, testContainer)
	}
	want := []string{"run --detach --privileged --systemd=always --label servicetest --label servicetest.scope=user img:tag"}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

func TestStartFails(t *testing.T) {
	cases := []struct {
		name   string
		scope  service.Scope
		out    string
		runErr error
		want   string
	}{
		{"podman fails", service.ScopeSystem, "", errors.New("no"), "start the system container: no"},
		{"no ID", service.ScopeSystem, "\n", nil, "not a container ID"},
		{"two lines", service.ScopeSystem, "warning\nabc\n", nil, "not a container ID"},
		{"bad scope", service.Scope(9), testContainer, nil, "neither ScopeSystem nor ScopeUser"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{handle: func([]string) (string, error) { return tc.out, tc.runErr }}
			_, err := harness{r: f}.start(t.Context(), "img", tc.scope)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRemoveAndCopyIn(t *testing.T) {
	f := &fakeRunner{}
	c := &container{h: harness{r: f}, id: testContainer}
	if err := c.copyIn(t.Context(), "/host/bin", "/usr/local/bin/x"); err != nil {
		t.Fatal(err)
	}
	if err := c.remove(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{"cp /host/bin c0ffee:/usr/local/bin/x", "rm --force c0ffee"}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Errorf("commands = %q, want %q", got, want)
	}

	f.handle = func([]string) (string, error) { return "", errors.New("gone") }
	if err := c.copyIn(t.Context(), "a", "b"); err == nil || !strings.Contains(err.Error(), "copy a into b: gone") {
		t.Errorf("copyIn err = %v", err)
	}
	if err := c.remove(t.Context()); err == nil || !strings.Contains(err.Error(), "remove container c0ffee: gone") {
		t.Errorf("remove err = %v", err)
	}
}

func TestExec(t *testing.T) {
	f := &fakeRunner{handle: func([]string) (string, error) { return "out", nil }}
	c := &container{h: harness{r: f}, id: testContainer, uid: "1000"}

	out, err := c.exec(t.Context(), execOptions{}, "true")
	if err != nil || string(out) != "out" {
		t.Fatalf("exec = %q, %v", out, err)
	}
	if _, err := c.exec(t.Context(), execOptions{stdin: strings.NewReader("{}"), user: true}, "driver"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exec c0ffee true",
		"exec --interactive --user svc --workdir /home/svc --env HOME=/home/svc" +
			" --env XDG_RUNTIME_DIR=/run/user/1000" +
			" --env DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus c0ffee driver",
	}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Errorf("commands = %q, want %q", got, want)
	}
	if got := f.calls[1].stdin; got != "{}" {
		t.Errorf("stdin = %q, want {}", got)
	}

	c.uid = ""
	if _, err := c.exec(t.Context(), execOptions{user: true}, "true"); err == nil {
		t.Error("exec as the user without a uid succeeded, want an error")
	}
}

func TestWaitRunningPollsUntilBooted(t *testing.T) {
	for _, final := range []string{"running", "degraded"} {
		t.Run(final, func(t *testing.T) {
			states := []string{"", "offline", "starting", final}
			f := &fakeRunner{handle: func([]string) (string, error) {
				s := states[0]
				states = states[1:]
				if s != "running" {
					return s + "\n", errors.New("exit status 1")
				}
				return s + "\n", nil
			}}
			sleep, n := countSleep(100)
			c := &container{h: harness{r: f, sleep: sleep}, id: testContainer, uid: "1000"}
			got, err := c.waitRunning(t.Context(), true)
			if err != nil || got != final {
				t.Fatalf("waitRunning = %q, %v, want %q", got, err, final)
			}
			if *n != 3 {
				t.Errorf("slept %d times, want 3", *n)
			}
			if cmd := f.commands()[0]; !strings.HasSuffix(cmd, "c0ffee systemctl --user is-system-running --wait") ||
				!strings.Contains(cmd, "--user svc") {
				t.Errorf("command = %q, want systemctl --user as svc", cmd)
			}
		})
	}
}

func TestWaitRunningGivesUp(t *testing.T) {
	f := &fakeRunner{handle: func([]string) (string, error) { return "starting\n", errors.New("exit status 1") }}
	sleep, _ := countSleep(2)
	c := &container{h: harness{r: f, sleep: sleep}, id: testContainer}
	_, err := c.waitRunning(t.Context(), false)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), `system manager did not finish booting, last state "starting"`) {
		t.Errorf("err = %v", err)
	}
	if got := f.commands()[0]; got != "exec c0ffee systemctl is-system-running --wait" {
		t.Errorf("command = %q", got)
	}
}

func TestLookupUID(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		runErr error
		want   string
	}{
		{"ok", "1000\n", nil, ""},
		{"fails", "", errors.New("no such user"), "no such user"},
		{"empty", "\n", nil, "id printed"},
		{"not a number", "svc\n", nil, "id printed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{handle: func([]string) (string, error) { return tc.out, tc.runErr }}
			c := &container{h: harness{r: f}, id: testContainer}
			err := c.lookupUID(t.Context())
			if tc.want == "" {
				if err != nil || c.uid != "1000" {
					t.Errorf("lookupUID = %v, uid %q, want 1000", err, c.uid)
				}
				if got := f.commands()[0]; got != "exec c0ffee id -u svc" {
					t.Errorf("command = %q", got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestWaitUserManager(t *testing.T) {
	states := []string{"inactive", "activating", "active"}
	f := &fakeRunner{handle: func([]string) (string, error) {
		s := states[0]
		states = states[1:]
		return s + "\n", nil
	}}
	c := &container{h: harness{r: f, sleep: noSleep}, id: testContainer, uid: "1000"}
	if err := c.waitUserManager(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.commands(); len(got) != 3 || got[0] != "exec c0ffee systemctl is-active user@1000.service" {
		t.Errorf("commands = %q", got)
	}

	f.handle = func([]string) (string, error) { return "failed\n", errors.New("exit status 3") }
	sleep, _ := countSleep(1)
	c.h.sleep = sleep
	err := c.waitUserManager(t.Context())
	if err == nil || !strings.Contains(err.Error(), `user@1000.service did not become active, last state "failed"`) {
		t.Errorf("err = %v", err)
	}
}

func TestDiagnostics(t *testing.T) {
	f := &fakeRunner{handle: func(args []string) (string, error) {
		argv := execArgv(args)
		switch {
		case len(argv) > 0 && argv[0] == "journalctl":
			return "journal line", nil
		case args[0] == "logs":
			return "console\n", nil
		default:
			return "inactive\n", errors.New("exit status 3")
		}
	}}
	c := &container{h: harness{r: f}, id: testContainer, uid: "1000"}

	sys := c.diagnostics(t.Context(), "demo.service", false)
	for _, want := range []string{
		"--- systemctl status --no-pager --full demo.service\ninactive\n(exit status 3)\n",
		"--- journalctl --no-pager --lines 200 --unit demo.service\njournal line\n",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("system diagnostics = %q, want it to contain %q", sys, want)
		}
	}
	usr := c.diagnostics(t.Context(), "demo.service", true)
	if !strings.Contains(usr, "--- systemctl --user status") || !strings.Contains(usr, "--user-unit demo.service") {
		t.Errorf("user diagnostics = %q", usr)
	}
	cmds := f.commands()
	if !strings.Contains(cmds[2], "--user svc") || strings.Contains(cmds[3], "--user svc") {
		t.Errorf("commands = %q, want systemctl --user as svc and journalctl as root", cmds)
	}

	boot := c.bootDiagnostics(t.Context())
	for _, want := range []string{"--- podman logs\nconsole\n", "--- systemctl --failed --no-pager\n", "--- journalctl --no-pager --boot --lines 200\n"} {
		if !strings.Contains(boot, want) {
			t.Errorf("boot diagnostics = %q, want it to contain %q", boot, want)
		}
	}
	if !slices.Contains(f.commands(), "logs --tail 200 c0ffee") {
		t.Errorf("commands = %q, want podman logs", f.commands())
	}

	ft := runTB(t, func(tb *fakeTB) { c.logDiagnostics(t.Context(), tb, "demo.service", false) })
	if !ft.logged("diagnostics of demo.service in container c0ffee") {
		t.Errorf("logs = %q", ft.logs)
	}
}

// bootRunner returns a fakeRunner for a container that boots, with fail
// overriding the answer to any command it returns ok false for.
func bootRunner(state string, override func(args []string) (string, error, bool)) *fakeRunner {
	return &fakeRunner{handle: func(args []string) (string, error) {
		if override != nil {
			if out, err, ok := override(args); ok {
				return out, err
			}
		}
		if args[0] == "run" {
			return testContainer + "\n", nil
		}
		argv := execArgv(args)
		switch {
		case slices.Contains(argv, "is-system-running"):
			return state + "\n", nil
		case slices.Equal(argv, []string{"id", "-u", "svc"}):
			return "1000\n", nil
		case slices.Contains(argv, "is-active"):
			return "active\n", nil
		}
		return "", nil
	}}
}

func TestBootSystem(t *testing.T) {
	f := bootRunner("running", nil)
	var c *container
	ft := runTB(t, func(tb *fakeTB) { c = boot(tb, harness{r: f, sleep: noSleep}, "img", service.ScopeSystem, time.Minute) })
	if ft.fatal != "" || len(ft.errs) > 0 {
		t.Fatalf("boot failed: %s %q", ft.fatal, ft.errs)
	}
	if c == nil || c.uid != "" {
		t.Errorf("container = %+v, want one with no uid", c)
	}
	want := []string{
		"run --detach --privileged --systemd=always --label servicetest --label servicetest.scope=system img",
		"exec c0ffee systemctl is-system-running --wait",
		"rm --force c0ffee",
	}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

func TestBootUser(t *testing.T) {
	f := bootRunner("running", nil)
	var c *container
	ft := runTB(t, func(tb *fakeTB) { c = boot(tb, harness{r: f, sleep: noSleep}, "img", service.ScopeUser, time.Minute) })
	if ft.fatal != "" {
		t.Fatalf("boot failed: %s", ft.fatal)
	}
	if c.uid != "1000" {
		t.Errorf("uid = %q, want 1000", c.uid)
	}
	got := f.commands()
	if len(got) != 6 || got[2] != "exec c0ffee id -u svc" || got[3] != "exec c0ffee systemctl is-active user@1000.service" ||
		!strings.HasSuffix(got[4], "c0ffee systemctl --user is-system-running --wait") || got[5] != "rm --force c0ffee" {
		t.Errorf("commands = %q", got)
	}
}

func TestBootLogsADegradedManager(t *testing.T) {
	f := bootRunner("degraded", nil)
	ft := runTB(t, func(tb *fakeTB) { boot(tb, harness{r: f, sleep: noSleep}, "img", service.ScopeUser, time.Minute) })
	if ft.fatal != "" {
		t.Fatalf("boot failed: %s", ft.fatal)
	}
	if !ft.logged("system manager of container c0ffee is degraded") || !ft.logged("user manager of container c0ffee is degraded") {
		t.Errorf("logs = %q, want both managers reported degraded", ft.logs)
	}
}

func TestBootFails(t *testing.T) {
	fail := errors.New("exit status 1")
	cases := []struct {
		name     string
		override func(args []string) (string, error, bool)
		want     string
		removed  bool
	}{
		{"start", func(args []string) (string, error, bool) { return "", fail, args[0] == "run" }, "start the user container", false},
		{"system manager", func(args []string) (string, error, bool) {
			return "maintenance\n", fail, slices.Contains(args, "is-system-running") && !slices.Contains(args, "--user")
		}, "system manager did not finish booting", true},
		{"uid", func(args []string) (string, error, bool) { return "", fail, slices.Contains(args, "id") }, "look up the uid", true},
		{"user manager unit", func(args []string) (string, error, bool) {
			return "failed\n", fail, slices.Contains(args, "is-active")
		}, "user@1000.service did not become active", true},
		{"user manager", func(args []string) (string, error, bool) {
			return "offline\n", fail, slices.Contains(args, "is-system-running") && slices.Contains(args, "--user")
		}, "user manager did not finish booting", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := bootRunner("running", tc.override)
			sleep, _ := countSleep(1)
			ft := runTB(t, func(tb *fakeTB) { boot(tb, harness{r: f, sleep: sleep}, "img", service.ScopeUser, time.Minute) })
			if !strings.Contains(ft.fatal, tc.want) {
				t.Errorf("fatal = %q, want it to contain %q", ft.fatal, tc.want)
			}
			if got := slices.Contains(f.commands(), "rm --force c0ffee"); got != tc.removed {
				t.Errorf("removed = %v, want %v", got, tc.removed)
			}
			if tc.removed && !ft.logged("boot diagnostics of container c0ffee") {
				t.Errorf("logs = %q, want the boot diagnostics", ft.logs)
			}
		})
	}
}

func TestBootReportsAFailedRemoval(t *testing.T) {
	f := bootRunner("running", func(args []string) (string, error, bool) {
		return "", errors.New("busy"), args[0] == "rm"
	})
	ft := runTB(t, func(tb *fakeTB) { boot(tb, harness{r: f, sleep: noSleep}, "img", service.ScopeSystem, time.Minute) })
	if len(ft.errs) != 1 || !strings.Contains(ft.errs[0], "remove container c0ffee: busy") {
		t.Errorf("errors = %q, want the failed removal", ft.errs)
	}
}

func TestSkipUnless(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		skip bool
	}{
		{"unset", nil, true},
		{"zero", map[string]string{enableEnv: "0"}, true},
		{"true", map[string]string{enableEnv: "true"}, true},
		{"one", map[string]string{enableEnv: "1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }
			ft := runTB(t, func(tb *fakeTB) { skipUnless(tb, lookup) })
			if ft.skipped != tc.skip {
				t.Errorf("skipped = %v, want %v", ft.skipped, tc.skip)
			}
		})
	}
}

func TestSkipUnlessEnabledReadsTheEnvironment(t *testing.T) {
	t.Setenv(enableEnv, "")
	ft := runTB(t, func(tb *fakeTB) { SkipUnlessEnabled(tb) })
	if !ft.skipped {
		t.Error("SkipUnlessEnabled did not skip with SERVICETEST empty")
	}
}

func TestRunSkipsUnlessEnabled(t *testing.T) {
	t.Setenv(enableEnv, "")
	t.Run("run", func(t *testing.T) {
		Run(t, Target{})
		t.Error("Run returned without skipping")
	})
}

func TestImageTag(t *testing.T) {
	a := imageTag([]byte("FROM x"), "")
	if !strings.HasPrefix(a, imageRepository+":") || len(a) != len(imageRepository)+1+16 {
		t.Errorf("imageTag = %q, want %s:<16 hex digits>", a, imageRepository)
	}
	if imageTag([]byte("FROM x"), "") != a {
		t.Error("imageTag is not deterministic")
	}
	if imageTag([]byte("FROM y"), "") == a || imageTag([]byte("FROM x"), "debian:13") == a {
		t.Error("imageTag ignores the file or the base")
	}
}

func TestContainerfilePinsTheBaseByDigest(t *testing.T) {
	s := string(containerfile)
	for _, want := range []string{
		"ARG BASE=docker.io/library/debian:trixie@sha256:",
		"FROM ${BASE}",
		"systemd systemd-sysv dbus dbus-user-session",
		"/var/lib/systemd/linger/svc",
		"STOPSIGNAL SIGRTMIN+3",
		`CMD ["/sbin/init"]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Containerfile lacks %q", want)
		}
	}
}

func TestEnsureImageReusesAnExistingImage(t *testing.T) {
	f := &fakeRunner{}
	tag, err := harness{r: f}.ensureImage(t.Context(), "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if want := imageTag(containerfile, ""); tag != want {
		t.Errorf("tag = %q, want %q", tag, want)
	}
	if got, want := f.commands(), []string{"image exists " + tag}; !slices.Equal(got, want) {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

func TestEnsureImageBuilds(t *testing.T) {
	for _, base := range []string{"", "docker.io/library/debian:13"} {
		t.Run("base="+base, func(t *testing.T) {
			dir := t.TempDir()
			f := &fakeRunner{handle: func(args []string) (string, error) {
				if args[0] == "image" {
					return "", errors.New("exit status 1")
				}
				return "", nil
			}}
			tag, err := harness{r: f}.ensureImage(t.Context(), base, dir)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "Containerfile")
			build := "build --tag " + tag + " --file " + file
			if base != "" {
				build += " --build-arg BASE=" + base
			}
			build += " " + dir
			if got := f.commands(); len(got) != 2 || got[1] != build {
				t.Errorf("commands = %q, want the build %q", got, build)
			}
			if got, err := os.ReadFile(file); err != nil || string(got) != string(containerfile) {
				t.Errorf("context Containerfile = %q, %v, want the embedded one", got, err)
			}
		})
	}
}

func TestEnsureImageFails(t *testing.T) {
	f := &fakeRunner{handle: func([]string) (string, error) { return "", errors.New("no network") }}
	_, err := harness{r: f}.ensureImage(t.Context(), "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "build image localhost/servicetest:") || !strings.Contains(err.Error(), "no network") {
		t.Errorf("err = %v", err)
	}

	_, err = harness{r: f}.ensureImage(t.Context(), "", filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "write the Containerfile") {
		t.Errorf("err = %v, want the write failure", err)
	}
}

func TestManagerName(t *testing.T) {
	if managerName(false) != "system" || managerName(true) != "user" {
		t.Error("managerName does not name the system and the user manager")
	}
}

func TestWriteSectionEndsWithANewline(t *testing.T) {
	var b strings.Builder
	writeSection(&b, "t", nil, nil)
	writeSection(&b, "u", []byte("x\n"), nil)
	if got, want := b.String(), "--- t\n--- u\nx\n"; got != want {
		t.Errorf("sections = %q, want %q", got, want)
	}
}

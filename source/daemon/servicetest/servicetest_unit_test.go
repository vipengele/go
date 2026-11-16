package servicetest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon/service"
	"go.vipengele.org/daemon/servicetest/internal/wire"
)

// fakeHost is a runner that plays podman, a booted container and the unit
// the driver manages in it. An install or a kill starts the service under a
// new PID, reported as starting for one status read; an install of
// crashBinary makes it crash-loop.
type fakeHost struct {
	mu   sync.Mutex
	cmds []string
	reqs []wire.Request
	// installed, enabled and crashing describe the unit; pid, restarts and
	// starting its service.
	installed, enabled, crashing bool
	pid, restarts, starting      int
	nextPID                      int
	// fault, when it returns ok, answers a command in place of the
	// emulation. req is the driver request, nil for any other command.
	fault func(f *fakeHost, args []string, req *wire.Request) (string, error, bool)
}

func newFakeHost() *fakeHost { return &fakeHost{nextPID: 100} }

func (f *fakeHost) run(_ context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	var req *wire.Request
	if stdin != nil {
		req = new(wire.Request)
		dec := json.NewDecoder(stdin)
		dec.DisallowUnknownFields()
		if err := dec.Decode(req); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, strings.Join(args, " "))
	if req != nil {
		f.reqs = append(f.reqs, *req)
	}
	if f.fault != nil {
		if out, err, ok := f.fault(f, args, req); ok {
			return []byte(out), err
		}
	}
	out, err := f.answer(args, req)
	return []byte(out), err
}

func (f *fakeHost) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.cmds)
}

// start runs the service under a new PID.
func (f *fakeHost) start() {
	f.pid = f.nextPID
	f.nextPID++
	f.starting = 1
}

func (f *fakeHost) answer(args []string, req *wire.Request) (string, error) {
	if args[0] == "run" {
		return testContainer + "\n", nil
	}
	if req != nil {
		return f.drive(*req)
	}
	argv := execArgv(args)
	switch {
	case len(argv) == 0:
		return "", nil
	case slices.Contains(argv, "is-system-running"):
		return "running\n", nil
	case argv[0] == "id":
		return "1000\n", nil
	case slices.Contains(argv, "is-active"):
		return "active\n", nil
	case argv[0] == "sh":
		if f.installed && !f.crashing && argv[len(argv)-1] == strconv.Itoa(f.pid) {
			f.start()
			f.restarts++
		}
	case argv[0] == "test" && f.installed:
		return "", errors.New("exit status 1")
	}
	return "", nil
}

func (f *fakeHost) drive(req wire.Request) (string, error) {
	switch req.Op {
	case wire.OpInstall:
		f.installed, f.enabled = true, true
		f.crashing = req.Config.Binary == crashBinary
		f.restarts = 0
		f.start()
		return "{}\n", nil
	case wire.OpUninstall:
		f.installed, f.enabled = false, false
		return "{}\n", nil
	}
	var st service.Status
	switch {
	case !f.installed:
		st = service.Status{State: service.StateNotInstalled}
	case f.crashing:
		st = service.Status{State: service.StateFailed, Enabled: true, Restarts: 1, Detail: "sampled.service failed"}
	case f.starting > 0:
		f.starting--
		st = service.Status{State: service.StateStarting, Enabled: true, Restarts: f.restarts}
	default:
		st = service.Status{State: service.StateRunning, Enabled: f.enabled, PID: f.pid, Restarts: f.restarts}
	}
	return response(wire.Response{Status: &st}), nil
}

// response encodes r as the driver writes it.
func response(r wire.Response) string {
	b, _ := json.Marshal(r)
	return string(b) + "\n"
}

// testTarget is a Target for scope with every option set.
func testTarget(scope service.Scope) Target {
	return Target{
		Binary:    "/host/sampled",
		Config:    service.Config{Name: "sampled", Binary: "/opt/sampled/bin/sampled", Scope: scope},
		Notify:    &Notify{Watchdog: time.Second},
		Hardening: &service.Hardening{NoNewPrivileges: true},
		Identity:  &spec.Identity{Name: "sampled"},
	}
}

// okBuild is a goBuild that succeeds without building.
func okBuild(context.Context, string) ([]byte, error) { return nil, nil }

func TestRunProvesAServiceInEachScope(t *testing.T) {
	for _, scope := range []service.Scope{service.ScopeSystem, service.ScopeUser} {
		name, _ := scopeName(scope)
		t.Run(name, func(t *testing.T) {
			f := newFakeHost()
			var built string
			h := harness{r: f, sleep: noSleep, goBuild: func(_ context.Context, out string) ([]byte, error) {
				built = out
				return nil, nil
			}}
			var ran []string
			t.Run("run", func(t *testing.T) {
				run(t, h, testTarget(scope))
				ran = append(ran, t.Name())
			})
			if len(ran) != 1 {
				t.Fatal("run did not return")
			}
			if filepath.Base(built) != "servicetest-driver" {
				t.Errorf("driver built at %q", built)
			}
			cmds := f.commands()
			for _, want := range []string{
				"exec c0ffee mkdir -p /opt/sampled/bin",
				"cp /host/sampled c0ffee:/opt/sampled/bin/sampled",
				"cp " + built + " c0ffee:" + driverPath,
				"exec c0ffee chmod 0755 /opt/sampled/bin/sampled " + driverPath,
				`exec c0ffee sh -c kill -9 "$1" sh 100`,
				"rm --force c0ffee",
			} {
				if !slices.Contains(cmds, want) {
					t.Errorf("commands lack %q:\n%s", want, strings.Join(cmds, "\n"))
				}
			}
			unit := "exec c0ffee test ! -e /etc/systemd/system/sampled.service"
			if scope == service.ScopeUser {
				unit = "exec c0ffee test ! -e /home/svc/.config/systemd/user/sampled.service"
			}
			if !slices.Contains(cmds, unit) {
				t.Errorf("commands lack %q", unit)
			}
			driverCalls := 0
			for _, c := range cmds {
				if strings.HasSuffix(c, "c0ffee "+driverPath) {
					driverCalls++
					if got := strings.Contains(c, "--user svc"); got != (scope == service.ScopeUser) {
						t.Errorf("driver call %q, want it as svc only for the user scope", c)
					}
				}
			}
			if driverCalls != len(f.reqs) {
				t.Errorf("%d driver calls, %d requests", driverCalls, len(f.reqs))
			}
			var ops []wire.Op
			for _, r := range f.reqs {
				if len(ops) == 0 || ops[len(ops)-1] != r.Op {
					ops = append(ops, r.Op)
				}
			}
			// Consecutive status reads collapse into one.
			wantOps := []wire.Op{
				wire.OpInstall, wire.OpStatus, wire.OpInstall, wire.OpStatus,
				wire.OpInstall, wire.OpStatus, wire.OpUninstall, wire.OpStatus,
			}
			if !slices.Equal(ops, wantOps) {
				t.Errorf("driver ops = %q, want %q", ops, wantOps)
			}
			r := f.reqs[0]
			if r.Config.Name != "sampled" || r.Config.Scope != scope || r.Notify == nil || r.Notify.Watchdog != time.Second ||
				r.Hardening == nil || !r.Hardening.NoNewPrivileges || r.Identity == nil || r.Identity.Name != "sampled" {
				t.Errorf("first request = %+v, want the target's config and options", r)
			}
			crash := slices.IndexFunc(f.reqs, func(r wire.Request) bool { return r.Config.Binary == crashBinary })
			if crash < 0 || f.reqs[crash].Op != wire.OpInstall || f.reqs[crash].Config.Name != "sampled" {
				t.Errorf("no install of %s under the same name: %+v", crashBinary, f.reqs)
			}
		})
	}
}

// step is one subtest the fake subtest ran.
type step struct {
	name string
	tb   *fakeTB
}

// runSuite runs the suite for target in a booted fake container on f,
// through a subtest that runs each body on a fakeTB, and returns the steps
// it ran and the fakeTB of the whole run.
func runSuite(t *testing.T, f *fakeHost, target Target, sleep func(context.Context, time.Duration) error) ([]step, *fakeTB) {
	t.Helper()
	c := &container{h: harness{r: f, sleep: sleep}, id: testContainer, scope: target.Config.Scope}
	if target.Config.Scope == service.ScopeUser {
		c.uid = "1000"
	}
	var steps []step
	sub := func(name string, body func(testing.TB)) bool {
		ft := runTB(t, func(tb *fakeTB) { body(tb) })
		steps = append(steps, step{name, ft})
		return ft.fatal == ""
	}
	parent := runTB(t, func(tb *fakeTB) { newSuite(c, target).run(tb, sub, "/host/driver") })
	return steps, parent
}

func stepNames(steps []step) []string {
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.name
	}
	return names
}

func TestSuitePassesEveryScenarioInOrder(t *testing.T) {
	steps, parent := runSuite(t, newFakeHost(), testTarget(service.ScopeSystem), noSleep)
	if parent.fatal != "" {
		t.Fatal(parent.fatal)
	}
	want := []string{"install", "kill", "reinstall", "crash_loop", "uninstall"}
	if got := stepNames(steps); !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
	for _, s := range steps {
		if s.tb.fatal != "" || len(s.tb.logs) > 0 {
			t.Errorf("%s: fatal %q, logs %q", s.name, s.tb.fatal, s.tb.logs)
		}
	}
}

// opIs reports whether req is a driver request for op.
func opIs(req *wire.Request, op wire.Op) bool { return req != nil && req.Op == op }

// nth returns a predicate true for the n-th request for op, counting from 1.
func nth(op wire.Op, n int) func(*wire.Request) bool {
	seen := 0
	return func(req *wire.Request) bool {
		if !opIs(req, op) {
			return false
		}
		seen++
		return seen == n
	}
}

func TestSuiteStopsAtTheFirstFailingScenario(t *testing.T) {
	exit1 := errors.New("exit status 1")
	cases := []struct {
		name   string
		fault  func() func(f *fakeHost, args []string, req *wire.Request) (string, error, bool)
		failAt string
		want   []string
	}{
		{"install refused", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return `{"error":"boom"}` + "\n", exit1, opIs(req, wire.OpInstall)
			}
		}, "install", []string{"driver install: boom"}},
		{"never running", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return response(wire.Response{Status: &service.Status{State: service.StateStarting}}), nil, opIs(req, wire.OpStatus)
			}
		}, "install", []string{"did not reach running and enabled", "state starting", "deadline exceeded"}},
		{"not enabled", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(f *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				if opIs(req, wire.OpInstall) {
					f.installed = true
					f.start()
					return "{}\n", nil, true
				}
				return "", nil, false
			}
		}, "install", []string{"did not reach running and enabled", "enabled false"}},
		{"kill fails", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, args []string, _ *wire.Request) (string, error, bool) {
				return "", errors.New("no such process"), slices.Contains(args, "sh")
			}
		}, "kill", []string{"kill -9 the main PID 100 of sampled.service: no such process"}},
		{"pid unchanged after kill", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, args []string, _ *wire.Request) (string, error, bool) {
				return "", nil, slices.Contains(args, "sh")
			}
		}, "kill", []string{"was not restarted after kill -9 of its main PID 100", "pid 100"}},
		{"reinstall refused", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			second := nth(wire.OpInstall, 2)
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return `{"error":"busy"}` + "\n", exit1, second(req)
			}
		}, "reinstall", []string{"driver install: busy"}},
		{"reinstall keeps the pid", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			second := nth(wire.OpInstall, 2)
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return "{}\n", nil, second(req)
			}
		}, "reinstall", []string{"under a main PID other than 101 after reinstall"}},
		{"crash loop never fails", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return "{}\n", nil, req != nil && req.Config.Binary == crashBinary && req.Op == wire.OpInstall
			}
		}, "crash_loop", []string{"did not report failed with restarts and a detail", "state running"}},
		{"crash loop install refused", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return `{"error":"job failed"}` + "\n", exit1, req != nil && req.Config.Binary == crashBinary && req.Op == wire.OpInstall
			}
		}, "crash_loop", []string{"did not report failed", "driver install: job failed"}},
		{"uninstall refused", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return `{"error":"denied"}` + "\n", exit1, opIs(req, wire.OpUninstall)
			}
		}, "uninstall", []string{"driver uninstall: denied"}},
		{"still installed", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return "{}\n", nil, opIs(req, wire.OpUninstall)
			}
		}, "uninstall", []string{"sampled.service is still installed after uninstall: state failed"}},
		{"status fails after uninstall", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(f *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return `{"error":"bus"}` + "\n", exit1, opIs(req, wire.OpStatus) && !f.installed
			}
		}, "uninstall", []string{"driver status: bus"}},
		{"unit file remains", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, args []string, _ *wire.Request) (string, error, bool) {
				return "", exit1, slices.Contains(args, "test")
			}
		}, "uninstall", []string{"the unit file /etc/systemd/system/sampled.service remains after uninstall: exit status 1"}},
		{"garbage and a non-zero exit", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return "panic: nil map\n", errors.New("exit status 2"), opIs(req, wire.OpInstall)
			}
		}, "install", []string{`driver install: unreadable response "panic: nil map\n"`, "exit status 2"}},
		{"garbage and a zero exit", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return "nonsense", nil, opIs(req, wire.OpStatus)
			}
		}, "install", []string{`driver status: unreadable response "nonsense"`}},
		{"empty object and a non-zero exit", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return "{}\n", exit1, opIs(req, wire.OpInstall)
			}
		}, "install", []string{"driver install: exit status 1"}},
		{"status without a status", func() func(*fakeHost, []string, *wire.Request) (string, error, bool) {
			return func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
				return "{}\n", nil, opIs(req, wire.OpStatus)
			}
		}, "install", []string{"the response carries no status"}},
	}
	order := []string{"install", "kill", "reinstall", "crash_loop", "uninstall"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHost()
			f.fault = tc.fault()
			sleep, _ := countSleep(50)
			steps, parent := runSuite(t, f, testTarget(service.ScopeSystem), sleep)
			if parent.fatal != "" {
				t.Fatalf("run failed outside a scenario: %s", parent.fatal)
			}
			want := order[:slices.Index(order, tc.failAt)+1]
			if got := stepNames(steps); !slices.Equal(got, want) {
				t.Fatalf("steps = %q, want %q", got, want)
			}
			last := steps[len(steps)-1].tb
			for _, w := range tc.want {
				if !strings.Contains(last.fatal, w) {
					t.Errorf("fatal = %q, want it to contain %q", last.fatal, w)
				}
			}
			if !last.logged("diagnostics of sampled.service in container c0ffee") {
				t.Errorf("logs = %q, want the unit's diagnostics", last.logs)
			}
			for _, s := range steps[:len(steps)-1] {
				if s.tb.fatal != "" {
					t.Errorf("%s failed: %s", s.name, s.tb.fatal)
				}
			}
		})
	}
}

func TestSuiteLogsUserDiagnostics(t *testing.T) {
	f := newFakeHost()
	f.fault = func(_ *fakeHost, _ []string, req *wire.Request) (string, error, bool) {
		return `{"error":"no bus"}` + "\n", errors.New("exit status 1"), opIs(req, wire.OpInstall)
	}
	steps, _ := runSuite(t, f, testTarget(service.ScopeUser), noSleep)
	if len(steps) != 1 || !strings.Contains(steps[0].tb.fatal, "no bus") {
		t.Fatalf("steps = %+v", steps)
	}
	if !slices.ContainsFunc(f.commands(), func(c string) bool {
		return strings.HasSuffix(c, "journalctl --no-pager --lines 200 --user-unit sampled.service")
	}) {
		t.Errorf("commands = %q, want the user unit's journal", f.commands())
	}
}

func TestSuiteFailsWhenTheBinariesCannotBePlaced(t *testing.T) {
	cases := []struct {
		name  string
		match func(args []string) bool
		want  string
	}{
		{"mkdir", func(a []string) bool { return slices.Contains(a, "mkdir") }, "create /opt/sampled/bin: denied"},
		{"binary", func(a []string) bool { return a[0] == "cp" && a[1] == "/host/sampled" }, "copy /host/sampled into /opt/sampled/bin/sampled: denied"},
		{"driver", func(a []string) bool { return a[0] == "cp" && a[1] == "/host/driver" }, "copy /host/driver into " + driverPath + ": denied"},
		{"chmod", func(a []string) bool { return slices.Contains(a, "chmod") }, "make /opt/sampled/bin/sampled and " + driverPath + " executable: denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHost()
			f.fault = func(_ *fakeHost, args []string, _ *wire.Request) (string, error, bool) {
				return "", errors.New("denied"), tc.match(args)
			}
			steps, parent := runSuite(t, f, testTarget(service.ScopeSystem), noSleep)
			if !strings.Contains(parent.fatal, tc.want) {
				t.Errorf("fatal = %q, want it to contain %q", parent.fatal, tc.want)
			}
			if len(steps) != 0 {
				t.Errorf("ran %q, want no scenario", stepNames(steps))
			}
		})
	}
}

func TestNewSuiteDefaultsTheTimeout(t *testing.T) {
	if s := newSuite(&container{}, Target{}); s.timeout != DefaultTimeout || s.user || s.unit != ".service" {
		t.Errorf("suite = %+v", s)
	}
	tg := testTarget(service.ScopeUser)
	tg.Timeout = time.Second
	if s := newSuite(&container{}, tg); s.timeout != time.Second || !s.user || s.unit != "sampled.service" {
		t.Errorf("suite = %+v", s)
	}
}

func TestSetup(t *testing.T) {
	tg := testTarget(service.ScopeSystem)
	tg.Driver = "/prebuilt/driver"
	var driver string
	built := false
	h := harness{goBuild: func(context.Context, string) ([]byte, error) { built = true; return nil, nil }}
	ft := runTB(t, func(tb *fakeTB) { driver = setup(tb, h, tg) })
	if ft.fatal != "" || driver != "/prebuilt/driver" || built {
		t.Errorf("setup = %q, built %v, fatal %q, want the prebuilt driver", driver, built, ft.fatal)
	}

	tg.Driver = ""
	ft = runTB(t, func(tb *fakeTB) { driver = setup(tb, h, tg) })
	if ft.fatal != "" || !built || filepath.Base(driver) != "servicetest-driver" {
		t.Errorf("setup = %q, built %v, fatal %q, want a built driver", driver, built, ft.fatal)
	}
}

func TestSetupFails(t *testing.T) {
	failBuild := harness{goBuild: func(context.Context, string) ([]byte, error) {
		return []byte("go: go.mod file not found in current directory or any parent directory\n"), errors.New("exit status 1")
	}}
	cases := []struct {
		name   string
		h      harness
		target func(Target) Target
		want   []string
	}{
		{"no binary", harness{goBuild: okBuild}, func(tg Target) Target { tg.Binary = ""; return tg }, []string{"Target.Binary is empty"}},
		{"relative binary", harness{goBuild: okBuild}, func(tg Target) Target { tg.Config.Binary = "bin/x"; return tg },
			[]string{`Target.Config.Binary "bin/x" is not an absolute path`}},
		{"bad scope", harness{goBuild: okBuild}, func(tg Target) Target { tg.Config.Scope = 4; return tg }, []string{"scope 4"}},
		{"build", failBuild, func(tg Target) Target { return tg }, []string{
			"build the driver " + driverPackage, "exit status 1: go: go.mod file not found",
			"does not vendor it", "set Target.Driver", "GOOS=linux CGO_ENABLED=0 go build " + driverPackage,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ft := runTB(t, func(tb *fakeTB) { setup(tb, tc.h, tc.target(testTarget(service.ScopeUser))) })
			for _, w := range tc.want {
				if !strings.Contains(ft.fatal, w) {
					t.Errorf("fatal = %q, want it to contain %q", ft.fatal, w)
				}
			}
		})
	}
}

func TestGoBuildDriverRunsTheGoCommand(t *testing.T) {
	standIn(t, "go")
	out, err := goBuildDriver(t.Context(), "/tmp/d")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "args=build,-o,/tmp/d,"+driverPackage+" stdin="; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestGoBuildDriverFailsWithoutGo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := goBuildDriver(t.Context(), "/tmp/d"); err == nil {
		t.Error("goBuildDriver without go on PATH succeeded")
	}
}

func TestPrepare(t *testing.T) {
	f := &fakeRunner{}
	var p plan
	ft := runTB(t, func(tb *fakeTB) { p = prepare(tb, harness{r: f}, service.ScopeUser, "b") })
	if ft.fatal != "" || p.name != "user" || p.image != imageTag(containerfile, "b") {
		t.Errorf("plan = %+v, fatal %q", p, ft.fatal)
	}
}

func TestPrepareFails(t *testing.T) {
	ft := runTB(t, func(tb *fakeTB) { prepare(tb, harness{r: &fakeRunner{}}, 5, "") })
	if !strings.Contains(ft.fatal, "scope 5") {
		t.Errorf("fatal = %q, want the bad scope", ft.fatal)
	}
	f := &fakeRunner{handle: func([]string) (string, error) { return "", errors.New("no network") }}
	ft = runTB(t, func(tb *fakeTB) { prepare(tb, harness{r: f}, service.ScopeSystem, "") })
	if !strings.Contains(ft.fatal, "no network") {
		t.Errorf("fatal = %q, want the build failure", ft.fatal)
	}
}

func TestRunScopeBootsOneContainer(t *testing.T) {
	f := bootRunner("running", nil)
	var got string
	t.Run("run", func(t *testing.T) {
		runScope(t, harness{r: f, sleep: noSleep}, service.ScopeUser, "", func(t *testing.T, c *container) {
			got = t.Name() + " uid=" + c.uid
		})
	})
	if want := "TestRunScopeBootsOneContainer/run/user uid=1000"; got != want {
		t.Errorf("body ran as %q, want %q", got, want)
	}
	if !slices.Contains(f.commands(), "rm --force c0ffee") {
		t.Errorf("commands = %q, want the container removed", f.commands())
	}
}

func TestDescribe(t *testing.T) {
	got := describe(service.Status{State: service.StateFailed, Enabled: true, PID: 7, Restarts: 2, Detail: "x"})
	if want := `state failed, enabled true, pid 7, restarts 2, detail "x"`; got != want {
		t.Errorf("describe = %q, want %q", got, want)
	}
}

package service

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/daemon"
)

// fakeSCM is an scm holding at most one service. It records every call in
// calls, as a name followed by the service name or argument where one helps.
type fakeSCM struct {
	calls []string

	elevated    bool
	elevatedErr error
	connectErr  error

	// exists reports whether the SCM holds the service; cfg, st and env are
	// its configuration, status and Environment value (nil when absent).
	exists bool
	cfg    scmConfig
	st     scmStatus
	env    []string

	actions     []scmRecoveryAction
	resetPeriod uint32
	nonCrash    bool

	// markedForDelete is the number of create calls that fail with
	// errSCMServiceMarkedForDelete before one succeeds; a negative value
	// fails every one.
	markedForDelete int
	// stopPolls is the number of queries after a stop that still report
	// SERVICE_STOP_PENDING.
	stopPolls int
	// refuseStops is the number of stop calls that fail with
	// errSCMServiceCannotAccept, and dropStops the number after them that
	// succeed and leave the state as it is. stopErr, when set, fails every
	// stop call after those.
	refuseStops int
	dropStops   int
	stopErr     error
	// relaunch makes the recovery restart fire: once relaunchPolls queries
	// have reported the service stopped with the restart code, the next one
	// finds it start-pending with relaunchPID.
	relaunch      bool
	relaunchPolls int
	// gonePolls is the number of opens after a delete that still find the
	// service; a negative value finds it forever.
	gonePolls int
	deleted   bool
	// openHandles counts the service handles not yet closed.
	openHandles int

	// fail maps a call, named as in calls without its argument ("open",
	// "close manager", "setEnvironment"), to the error it returns once
	// failAfter[call] calls of it have succeeded.
	fail      map[string]error
	failAfter map[string]int
}

func (f *fakeSCM) record(call string) { f.calls = append(f.calls, call) }

// injected is the error fail sets up for call, or nil when this call
// succeeds.
func (f *fakeSCM) injected(call string) error {
	err, ok := f.fail[call]
	if !ok {
		return nil
	}
	if f.failAfter[call] > 0 {
		f.failAfter[call]--
		return nil
	}
	return err
}

// failOn makes call fail with err after the first after calls of it have
// succeeded.
func (f *fakeSCM) failOn(call string, after int, err error) {
	if f.fail == nil {
		f.fail, f.failAfter = map[string]error{}, map[string]int{}
	}
	f.fail[call], f.failAfter[call] = err, after
}

type fakeSCMAPI struct{ f *fakeSCM }

func (a fakeSCMAPI) elevated() (bool, error) {
	a.f.record("elevated")
	return a.f.elevated, a.f.elevatedErr
}

func (a fakeSCMAPI) connect(access scmAccess) (scmManager, error) {
	if access == scmQuery {
		a.f.record("connect query")
	} else {
		a.f.record("connect control")
	}
	if a.f.connectErr != nil {
		return nil, a.f.connectErr
	}
	return fakeManager(a), nil
}

type fakeManager struct{ f *fakeSCM }

func (m fakeManager) open(name string) (scmService, error) {
	f := m.f
	f.record("open " + name)
	if err := f.injected("open"); err != nil {
		return nil, err
	}
	if f.deleted {
		if f.gonePolls == 0 {
			f.exists = false
			f.deleted = false
		} else if f.gonePolls > 0 {
			f.gonePolls--
		}
	}
	if !f.exists {
		return nil, errSCMServiceDoesNotExist
	}
	f.openHandles++
	return fakeService(m), nil
}

func (m fakeManager) create(name string, cfg scmConfig) (scmService, error) {
	f := m.f
	f.record("create " + name)
	if err := f.injected("create"); err != nil {
		return nil, err
	}
	if f.markedForDelete != 0 {
		if f.markedForDelete > 0 {
			f.markedForDelete--
		}
		return nil, errSCMServiceMarkedForDelete
	}
	f.exists = true
	f.cfg = cfg
	f.st = scmStatus{State: scmStopped, Win32ExitCode: uint32(errSCMServiceNeverStarted)}
	f.openHandles++
	return fakeService(m), nil
}

func (m fakeManager) close() error {
	m.f.record("close manager")
	return m.f.injected("close manager")
}

type fakeService struct{ f *fakeSCM }

func (s fakeService) config() (scmConfig, error) {
	s.f.record("config")
	if err := s.f.injected("config"); err != nil {
		return scmConfig{}, err
	}
	return s.f.cfg, nil
}

func (s fakeService) updateConfig(cfg scmConfig) error {
	s.f.record("updateConfig")
	if err := s.f.injected("updateConfig"); err != nil {
		return err
	}
	s.f.cfg = cfg
	return nil
}

func (s fakeService) setRecoveryActions(actions []scmRecoveryAction, resetPeriod uint32) error {
	s.f.record("setRecoveryActions")
	if err := s.f.injected("setRecoveryActions"); err != nil {
		return err
	}
	s.f.actions = slices.Clone(actions)
	s.f.resetPeriod = resetPeriod
	return nil
}

func (s fakeService) setRecoveryOnNonCrashFailures(on bool) error {
	s.f.record("setRecoveryOnNonCrashFailures")
	if err := s.f.injected("setRecoveryOnNonCrashFailures"); err != nil {
		return err
	}
	s.f.nonCrash = on
	return nil
}

func (s fakeService) setEnvironment(entries []string) error {
	s.f.record("setEnvironment " + strings.Join(entries, ","))
	if err := s.f.injected("setEnvironment"); err != nil {
		return err
	}
	s.f.env = slices.Clone(entries)
	return nil
}

func (s fakeService) deleteEnvironment() error {
	s.f.record("deleteEnvironment")
	if err := s.f.injected("deleteEnvironment"); err != nil {
		return err
	}
	s.f.env = nil
	return nil
}

func (s fakeService) start() error {
	s.f.record("start")
	if err := s.f.injected("start"); err != nil {
		return err
	}
	if s.f.st.State == scmRunning {
		return errSCMServiceAlreadyRunning
	}
	s.f.st = scmStatus{State: scmRunning, ProcessID: 4242}
	return nil
}

func (s fakeService) stop() (scmStatus, error) {
	f := s.f
	f.record("stop")
	if err := f.injected("stop"); err != nil {
		return scmStatus{}, err
	}
	if f.st.State == scmStopped {
		return f.st, errSCMServiceNotActive
	}
	if f.refuseStops > 0 {
		f.refuseStops--
		return f.st, errSCMServiceCannotAccept
	}
	if f.dropStops > 0 {
		f.dropStops--
		return f.st, nil
	}
	if f.stopErr != nil {
		return scmStatus{}, f.stopErr
	}
	f.st = scmStatus{State: scmStopPending}
	return f.st, nil
}

// relaunchPID is the process ID of the service the fake's recovery restart
// starts.
const relaunchPID = 5150

func (s fakeService) query() (scmStatus, error) {
	s.f.record("query")
	if err := s.f.injected("query"); err != nil {
		return scmStatus{}, err
	}
	if s.f.relaunch && inRestartDelay(s.f.st) {
		if s.f.relaunchPolls == 0 {
			s.f.relaunch = false
			s.f.st = scmStatus{State: scmStartPending, ProcessID: relaunchPID}
		} else {
			s.f.relaunchPolls--
		}
	}
	if s.f.st.State == scmStopPending {
		if s.f.stopPolls == 0 {
			s.f.st = scmStatus{State: scmStopped}
		} else {
			s.f.stopPolls--
		}
	}
	return s.f.st, nil
}

func (s fakeService) delete() error {
	s.f.record("delete")
	if err := s.f.injected("delete"); err != nil {
		return err
	}
	s.f.deleted = true
	return nil
}

func (s fakeService) close() error {
	s.f.record("close")
	s.f.openHandles--
	return s.f.injected("close")
}

// newTestWindows returns a driver for the service "demo" over a fake SCM
// holding no service, in an elevated process, and its fake. The driver's
// sleep returns at once and counts its calls in *sleeps.
func newTestWindows(cfg Config) (*windowsDriver, *fakeSCM, *int) {
	f := &fakeSCM{elevated: true}
	sleeps := new(int)
	w := &windowsDriver{
		cfg: cfg,
		scm: fakeSCMAPI{f},
		sleep: func(ctx context.Context, d time.Duration) error {
			if d != windowsPollInterval {
				panic("unexpected sleep " + d.String())
			}
			*sleeps++
			return ctx.Err()
		},
	}
	return w, f, sleeps
}

func demoWindowsConfig() Config {
	return Config{
		Name:        "demo",
		Description: "Demo service",
		Binary:      `C:\Program Files\Demo\demo.exe`,
		Args:        []string{"run", "--config", `C:\ProgramData\Demo\demo config.json`},
	}
}

// installed puts a service into f as if an earlier Install registered it.
func installed(f *fakeSCM, st scmStatus) {
	f.exists = true
	f.st = st
	f.cfg = scmConfig{
		ServiceType:      scmServiceWin32OwnProcess,
		StartType:        scmStartAutomatic,
		ErrorControl:     scmErrorNormal,
		BinaryPathName:   `C:\old.exe`,
		LoadOrderGroup:   "group",
		Dependencies:     []string{"Tcpip"},
		ServiceStartName: `NT AUTHORITY\LocalService`,
		DisplayName:      "old",
		Description:      "old",
		SidType:          1,
		DelayedAutoStart: true,
	}
}

func TestWindowsEscapeArgMatchesEscapeArg(t *testing.T) {
	// The vectors of syscall.EscapeArg's own tests, and tabs, quotes beside
	// spaces and backslashes before a quote in a quoted word.
	tests := []struct{ in, want string }{
		{``, `""`},
		{`a`, `a`},
		{` `, `" "`},
		{"\t", "\"\t\""},
		{"a\tb", "\"a\tb\""},
		{`\`, `\`},
		{`"`, `\"`},
		{`\"`, `\\\"`},
		{`\\"`, `\\\\\"`},
		{`\\ `, `"\\ "`},
		{` \\`, `" \\\\"`},
		{`a `, `"a "`},
		{`a b`, `"a b"`},
		{`a "b" c`, `"a \"b\" c"`},
		{`a\"b c\`, `"a\\\"b c\\"`},
		{`a"b`, `a\"b`},
		{`C:\`, `C:\`},
		{`C:\Program Files (x32)\Common\`, `"C:\Program Files (x32)\Common\\"`},
		{`C:\Users\Игорь\`, `C:\Users\Игорь\`},
		{`Андрей\file`, `Андрей\file`},
		{`C:\Windows\temp`, `C:\Windows\temp`},
		{`c:\temp\newfile`, `c:\temp\newfile`},
		{`\\?\C:\Windows`, `\\?\C:\Windows`},
		{`\\?\`, `\\?\`},
		{`\\.\C:\Windows\`, `\\.\C:\Windows\`},
		{`\\server\share\file`, `\\server\share\file`},
		{`\\newserver\tempshare\really.txt`, `\\newserver\tempshare\really.txt`},
	}
	for _, tt := range tests {
		if got := windowsEscapeArg(tt.in); got != tt.want {
			t.Errorf("windowsEscapeArg(%#q) = %#q, want %#q", tt.in, got, tt.want)
		}
	}
}

func TestWindowsCommandLineQuotesEveryWord(t *testing.T) {
	cfg := Config{Binary: `C:\Program Files\Demo\demo.exe`, Args: []string{"run", "", `a "b"`, `dir\`}}
	want := `"C:\Program Files\Demo\demo.exe" run "" "a \"b\"" dir\`
	if got := windowsCommandLine(cfg); got != want {
		t.Errorf("windowsCommandLine = %#q, want %#q", got, want)
	}
}

func TestWindowsInstallCreatesTheService(t *testing.T) {
	cfg := demoWindowsConfig()
	cfg.Env = map[string]string{"B": "2", "A": "x=1"}
	w, f, _ := newTestWindows(cfg)
	if err := w.Install(t.Context()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := scmConfig{
		ServiceType:      scmServiceWin32OwnProcess,
		StartType:        scmStartAutomatic,
		ErrorControl:     scmErrorNormal,
		BinaryPathName:   `"C:\Program Files\Demo\demo.exe" run --config "C:\ProgramData\Demo\demo config.json"`,
		ServiceStartName: windowsLocalSystem,
		DisplayName:      "demo",
		Description:      "Demo service",
	}
	if !equalSCMConfig(f.cfg, want) {
		t.Errorf("config = %+v, want %+v", f.cfg, want)
	}
	if !slices.Equal(f.env, []string{"A=x=1", "B=2"}) {
		t.Errorf("Environment = %q, want [A=x=1 B=2]", f.env)
	}
	wantActions := []scmRecoveryAction{{Type: scmActionRestart, Delay: 5 * time.Second}}
	if !slices.Equal(f.actions, wantActions) || f.resetPeriod != 86400 {
		t.Errorf("recovery = %+v reset %d, want %+v reset 86400", f.actions, f.resetPeriod, wantActions)
	}
	if !f.nonCrash {
		t.Error("recovery on non-crash failures is off")
	}
	if f.st.State != scmRunning {
		t.Errorf("state = %d, want running", f.st.State)
	}
	wantCalls := []string{
		"elevated", "connect control", "open demo", "create demo",
		"setRecoveryActions", "setRecoveryOnNonCrashFailures", "setEnvironment A=x=1,B=2",
		"query", "start", "close", "close manager",
	}
	if !slices.Equal(f.calls, wantCalls) {
		t.Errorf("calls =\n%q\nwant\n%q", f.calls, wantCalls)
	}
	if f.openHandles != 0 {
		t.Errorf("%d service handles left open", f.openHandles)
	}
}

func equalSCMConfig(a, b scmConfig) bool {
	return reflect.DeepEqual(a, b)
}

func TestWindowsReinstallRewritesTheConfig(t *testing.T) {
	tests := []struct {
		name        string
		user        string
		wantAccount string
	}{
		{"empty User is LocalSystem", "", windowsLocalSystem},
		{"another account", `NT SERVICE\demo`, `NT SERVICE\demo`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := demoWindowsConfig()
			cfg.User = tt.user
			w, f, _ := newTestWindows(cfg)
			installed(f, scmStatus{State: scmRunning, ProcessID: 7})
			f.env = []string{"OLD=1"}
			if err := w.Install(t.Context()); err != nil {
				t.Fatalf("Install: %v", err)
			}
			want := scmConfig{
				ServiceType:      scmServiceWin32OwnProcess,
				StartType:        scmStartAutomatic,
				ErrorControl:     scmErrorNormal,
				BinaryPathName:   windowsCommandLine(cfg),
				LoadOrderGroup:   "group",
				Dependencies:     []string{"Tcpip"},
				ServiceStartName: tt.wantAccount,
				DisplayName:      "demo",
				Description:      "Demo service",
				SidType:          1,
				DelayedAutoStart: true,
			}
			if !equalSCMConfig(f.cfg, want) {
				t.Errorf("config = %+v, want %+v", f.cfg, want)
			}
			if f.env != nil {
				t.Errorf("Environment = %q, want it deleted", f.env)
			}
			if !slices.Contains(f.calls, "deleteEnvironment") {
				t.Error("deleteEnvironment not called")
			}
			// The running copy stops before the service starts on its new
			// configuration.
			stop, start := slices.Index(f.calls, "stop"), slices.Index(f.calls, "start")
			if stop < 0 || start < stop {
				t.Errorf("calls = %q, want stop before start", f.calls)
			}
			if slices.Contains(f.calls, "create demo") {
				t.Error("an installed service was created again")
			}
			if f.st.State != scmRunning || f.openHandles != 0 {
				t.Errorf("state %d with %d open handles, want running with none", f.st.State, f.openHandles)
			}
		})
	}
}

func TestWindowsInstallWaitsOutAServiceMarkedForDeletion(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	f.markedForDelete = 3
	if err := w.Install(t.Context()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if *sleeps != 3 {
		t.Errorf("slept %d times, want 3", *sleeps)
	}
	if !f.exists || f.st.State != scmRunning {
		t.Errorf("exists %v, state %d, want a running service", f.exists, f.st.State)
	}
}

func TestWindowsInstallTimesOutOnAServiceMarkedForDeletion(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	f.markedForDelete = -1
	err := w.Install(t.Context())
	if err == nil || !strings.Contains(err.Error(), "marked for deletion") {
		t.Fatalf("Install = %v, want a timeout on the deletion", err)
	}
	if want := int(windowsWaitTimeout / windowsPollInterval); *sleeps != want {
		t.Errorf("slept %d times, want %d", *sleeps, want)
	}
	if f.exists {
		t.Error("the service was created")
	}
}

func TestWindowsMutatingMethodsNeedElevation(t *testing.T) {
	methods := map[string]func(*windowsDriver, context.Context) error{
		"Prepare":   (*windowsDriver).Prepare,
		"Install":   (*windowsDriver).Install,
		"Uninstall": (*windowsDriver).Uninstall,
		"Start":     (*windowsDriver).Start,
		"Stop":      (*windowsDriver).Stop,
	}
	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			w, f, _ := newTestWindows(demoWindowsConfig())
			f.elevated = false
			installed(f, scmStatus{State: scmRunning, ProcessID: 7})
			err := call(w, t.Context())
			if !errors.Is(err, ErrPrivilegesRequired) {
				t.Fatalf("%s = %v, want ErrPrivilegesRequired", name, err)
			}
			if !slices.Equal(f.calls, []string{"elevated"}) {
				t.Errorf("calls = %q, want only the elevation check", f.calls)
			}
		})
	}
}

func TestWindowsElevationCheckFailure(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	f.elevatedErr = errSCMAccessDenied
	if err := w.Start(t.Context()); !errors.Is(err, errSCMAccessDenied) || errors.Is(err, ErrPrivilegesRequired) {
		t.Fatalf("Start = %v, want the elevation check's error", err)
	}
}

func TestWindowsConnectFailureIsUnreachable(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	f.connectErr = errSCMAccessDenied
	if err := w.Install(t.Context()); !errors.Is(err, ErrManagerUnreachable) {
		t.Errorf("Install = %v, want ErrManagerUnreachable", err)
	}
	st, err := w.Status(t.Context())
	if !errors.Is(err, ErrManagerUnreachable) || st.State != StateUnknown {
		t.Errorf("Status = %+v, %v, want StateUnknown and ErrManagerUnreachable", st, err)
	}
}

func TestWindowsMethodsOfAMissingServiceAreNotInstalled(t *testing.T) {
	methods := map[string]func(*windowsDriver, context.Context) error{
		"Uninstall": (*windowsDriver).Uninstall,
		"Start":     (*windowsDriver).Start,
		"Stop":      (*windowsDriver).Stop,
	}
	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			w, _, _ := newTestWindows(demoWindowsConfig())
			if err := call(w, t.Context()); !errors.Is(err, ErrNotInstalled) {
				t.Fatalf("%s = %v, want ErrNotInstalled", name, err)
			}
		})
	}
}

func TestWindowsPrepareCreatesNothing(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	if err := w.Prepare(t.Context()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !slices.Equal(f.calls, []string{"elevated"}) {
		t.Errorf("calls = %q, want only the elevation check", f.calls)
	}
}

func TestWindowsStartStartsTheService(t *testing.T) {
	for _, state := range []scmState{scmStopped, scmRunning} {
		w, f, _ := newTestWindows(demoWindowsConfig())
		installed(f, scmStatus{State: state})
		if err := w.Start(t.Context()); err != nil {
			t.Fatalf("Start from state %d: %v", state, err)
		}
		if f.st.State != scmRunning || f.openHandles != 0 {
			t.Errorf("state %d with %d open handles, want running with none", f.st.State, f.openHandles)
		}
	}
}

func TestWindowsStopWaitsUntilStopped(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmRunning, ProcessID: 7})
	f.stopPolls = 2
	if err := w.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if f.st.State != scmStopped || *sleeps != 2 {
		t.Errorf("state %d after %d sleeps, want stopped after 2", f.st.State, *sleeps)
	}
}

func TestWindowsStopOfAStoppedServiceSucceeds(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmStopped})
	if err := w.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if slices.Contains(f.calls, "stop") {
		t.Error("the stop control was sent to a stopped service")
	}
}

func TestWindowsStopTimesOut(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmRunning, ProcessID: 7})
	f.stopPolls = 1 << 30
	if err := w.Stop(t.Context()); err == nil || !strings.Contains(err.Error(), "not stopped") {
		t.Fatalf("Stop = %v, want a timeout", err)
	}
}

// restartCodeStop is the status of a service stopped with the restart code,
// inside its restart delay.
func restartCodeStop() scmStatus {
	return scmStatus{
		State:                   scmStopped,
		Win32ExitCode:           uint32(errSCMServiceSpecificError),
		ServiceSpecificExitCode: daemon.WindowsRestartExitCode,
	}
}

func TestWindowsStopInsideTheRestartDelayOutlastsIt(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	installed(f, restartCodeStop())
	if err := w.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if want := int((windowsRestartDelay + windowsRestartMargin) / windowsPollInterval); *sleeps != want {
		t.Errorf("slept %d times, want %d, the restart delay plus the margin", *sleeps, want)
	}
	if slices.Contains(f.calls, "stop") {
		t.Errorf("calls = %q, want no stop control to a service that stayed stopped", f.calls)
	}
	if f.st != restartCodeStop() || f.openHandles != 0 {
		t.Errorf("status %+v with %d open handles, want it still stopped with the restart code and none open",
			f.st, f.openHandles)
	}
}

func TestWindowsStopInsideTheRestartDelayStopsTheRelaunchedService(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	installed(f, restartCodeStop())
	f.relaunch = true
	f.relaunchPolls = 10
	f.refuseStops = 2
	if err := w.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if f.relaunch {
		t.Fatal("the recovery restart never fired")
	}
	if *sleeps >= int((windowsRestartDelay+windowsRestartMargin)/windowsPollInterval) {
		t.Errorf("slept %d times, want the wait to end once the service was relaunched", *sleeps)
	}
	if n := countCalls(f.calls, "stop"); n != 3 {
		t.Errorf("sent the stop control %d times, want 3: twice refused while start-pending, then taken", n)
	}
	if f.st.State != scmStopped {
		t.Fatalf("state %d, want stopped", f.st.State)
	}
	f.elevated = false
	st, err := w.Status(t.Context())
	if err != nil || st.State != StateStopped {
		t.Errorf("Status after Stop = %+v, %v, want %v", st, err, StateStopped)
	}
}

func TestWindowsInstallAndUninstallOutlastTheRestartDelay(t *testing.T) {
	methods := map[string]func(*windowsDriver, context.Context) error{
		"Install":   (*windowsDriver).Install,
		"Uninstall": (*windowsDriver).Uninstall,
	}
	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			w, f, _ := newTestWindows(demoWindowsConfig())
			installed(f, restartCodeStop())
			f.relaunch = true
			f.relaunchPolls = 4
			if err := call(w, t.Context()); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if f.relaunch || !slices.Contains(f.calls, "stop") {
				t.Errorf("calls = %q, want the relaunched service stopped", f.calls)
			}
		})
	}
}

func TestWindowsStopResendsAStopTheServiceDidNotTake(t *testing.T) {
	tests := []struct {
		name          string
		state         scmState
		refuse, drop  int
		wantStopCalls int
	}{
		{"refused while start-pending", scmStartPending, 3, 0, 4},
		{"taken but not acted on", scmRunning, 0, 2, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, f, _ := newTestWindows(demoWindowsConfig())
			installed(f, scmStatus{State: tt.state, ProcessID: 7})
			f.refuseStops, f.dropStops = tt.refuse, tt.drop
			if err := w.Stop(t.Context()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			if n := countCalls(f.calls, "stop"); n != tt.wantStopCalls {
				t.Errorf("sent the stop control %d times, want %d", n, tt.wantStopCalls)
			}
			if f.st.State != scmStopped {
				t.Errorf("state %d, want stopped", f.st.State)
			}
		})
	}
}

func TestWindowsStopTimesOutOnAStopNeverTaken(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmStartPending, ProcessID: 7})
	f.refuseStops = 1 << 30
	if err := w.Stop(t.Context()); err == nil || !strings.Contains(err.Error(), "not stopped") {
		t.Fatalf("Stop = %v, want a timeout", err)
	}
	if want := int(windowsWaitTimeout / windowsPollInterval); *sleeps != want {
		t.Errorf("slept %d times, want %d", *sleeps, want)
	}
}

func TestWindowsStopInsideTheRestartDelayTimesOutOnARelaunchThatDoesNotStop(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, restartCodeStop())
	f.relaunch = true
	f.relaunchPolls = 2
	f.stopPolls = 1 << 30
	if err := w.Stop(t.Context()); err == nil || !strings.Contains(err.Error(), "not stopped") {
		t.Fatalf("Stop = %v, want a timeout", err)
	}
}

func TestWindowsStopFailsAtOnceOnAStopError(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmRunning, ProcessID: 7})
	f.stopErr = errSCMAccessDenied
	if err := w.Stop(t.Context()); !errors.Is(err, errSCMAccessDenied) {
		t.Fatalf("Stop = %v, want the stop control's error", err)
	}
	if *sleeps != 0 {
		t.Errorf("slept %d times, want none", *sleeps)
	}
}

func TestWindowsStopTakesTheContextsEnd(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, restartCodeStop())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := w.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v, want context.Canceled", err)
	}
}

// countCalls is the number of calls equal to call.
func countCalls(calls []string, call string) int {
	n := 0
	for _, c := range calls {
		if c == call {
			n++
		}
	}
	return n
}

func TestWindowsUninstallWaitsUntilGone(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmRunning, ProcessID: 7})
	f.gonePolls = 3
	if err := w.Uninstall(t.Context()); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if f.exists {
		t.Error("the service is still registered")
	}
	if *sleeps != 3 {
		t.Errorf("slept %d times, want 3", *sleeps)
	}
	stop, del := slices.Index(f.calls, "stop"), slices.Index(f.calls, "delete")
	if stop < 0 || del < stop {
		t.Errorf("calls = %q, want stop before delete", f.calls)
	}
	// The driver's own handle is closed before it looks for the service to
	// disappear.
	if i := slices.Index(f.calls[del:], "close"); i != 1 {
		t.Errorf("calls = %q, want close right after delete", f.calls)
	}
	if f.openHandles != 0 {
		t.Errorf("%d service handles left open", f.openHandles)
	}
}

func TestWindowsUninstallTimesOut(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmStopped})
	f.gonePolls = -1
	if err := w.Uninstall(t.Context()); err == nil || !strings.Contains(err.Error(), "still registered") {
		t.Fatalf("Uninstall = %v, want a timeout", err)
	}
	if f.openHandles != 0 {
		t.Errorf("%d service handles left open", f.openHandles)
	}
}

func TestWindowsStatusMapsTheSCMStatus(t *testing.T) {
	specific := uint32(errSCMServiceSpecificError)
	tests := []struct {
		name   string
		st     scmStatus
		polls  int
		want   State
		pid    int
		detail string
	}{
		{"running", scmStatus{State: scmRunning, ProcessID: 4242}, 0, StateRunning, 4242, ""},
		{"start pending", scmStatus{State: scmStartPending, ProcessID: 9}, 0, StateStarting, 9, ""},
		{"stop pending", scmStatus{State: scmStopPending, ProcessID: 9}, 1, StateStopped, 0, ""},
		{"paused", scmStatus{State: scmPaused, ProcessID: 3}, 0, StateRunning, 3, ""},
		{"clean stop", scmStatus{State: scmStopped}, 0, StateStopped, 0, ""},
		{"never started", scmStatus{State: scmStopped, Win32ExitCode: uint32(errSCMServiceNeverStarted)}, 0, StateStopped, 0, ""},
		{"specific 0", scmStatus{State: scmStopped, Win32ExitCode: specific}, 0, StateStopped, 0, ""},
		{"restart code", scmStatus{State: scmStopped, Win32ExitCode: specific, ServiceSpecificExitCode: daemon.WindowsRestartExitCode}, 0, StateStarting, 0, ""},
		{"failure code", scmStatus{State: scmStopped, Win32ExitCode: specific, ServiceSpecificExitCode: daemon.WindowsFailureExitCode}, 0, StateFailed, 0, "service-specific exit code 1"},
		{"win32 code", scmStatus{State: scmStopped, Win32ExitCode: 1067}, 0, StateFailed, 0, "Win32 exit code 1067"},
		{"unknown state", scmStatus{State: 99}, 0, StateUnknown, 0, "state 99"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, f, _ := newTestWindows(demoWindowsConfig())
			f.elevated = false
			installed(f, tt.st)
			f.stopPolls = tt.polls
			st, err := w.Status(t.Context())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st.State != tt.want || st.PID != tt.pid || st.Restarts != 0 || !st.Enabled {
				t.Errorf("Status = %+v, want state %v, pid %d, enabled, no restarts", st, tt.want, tt.pid)
			}
			if !strings.Contains(st.Detail, tt.detail) || tt.detail == "" && st.Detail != "" {
				t.Errorf("Detail = %q, want it to hold %q", st.Detail, tt.detail)
			}
			if f.calls[0] != "connect query" || slices.Contains(f.calls, "elevated") {
				t.Errorf("calls = %q, want a query connection and no elevation check", f.calls)
			}
		})
	}
}

func TestWindowsStatusOfAMissingServiceIsNotInstalled(t *testing.T) {
	w, _, _ := newTestWindows(demoWindowsConfig())
	st, err := w.Status(t.Context())
	if err != nil || st.State != StateNotInstalled {
		t.Fatalf("Status = %+v, %v, want StateNotInstalled", st, err)
	}
}

func TestWindowsStatusReportsManualStartAsDisabled(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmStopped})
	f.cfg.StartType = 3
	st, err := w.Status(t.Context())
	if err != nil || st.Enabled {
		t.Fatalf("Status = %+v, %v, want not enabled", st, err)
	}
}

func TestSCMErrnoErrorNamesEveryCodeTheDriverTellsApart(t *testing.T) {
	tests := []struct {
		code scmErrno
		want string
	}{
		{errSCMAccessDenied, "Access is denied."},
		{errSCMServiceAlreadyRunning, "An instance of the service is already running."},
		{errSCMServiceDoesNotExist, "The specified service does not exist as an installed service."},
		{errSCMServiceCannotAccept, "The service cannot accept control messages at this time."},
		{errSCMServiceNotActive, "The service has not been started."},
		{errSCMServiceMarkedForDelete, "The specified service has been marked for deletion."},
		{errSCMServiceSpecificError, "Win32 error 1066"},
		{errSCMServiceNeverStarted, "Win32 error 1077"},
		{0, "Win32 error 0"},
		{4294967295, "Win32 error 4294967295"},
	}
	for _, tt := range tests {
		if got := tt.code.Error(); got != tt.want {
			t.Errorf("scmErrno(%d).Error() = %q, want %q", uint32(tt.code), got, tt.want)
		}
	}
}

// errInjected is the error a test makes a call of the fake SCM fail with.
const errInjected scmErrno = 9999

// windowsCall is a method of the driver as a function returning its error.
type windowsCall func(*windowsDriver, context.Context) error

func windowsStatusCall(w *windowsDriver, ctx context.Context) error {
	_, err := w.Status(ctx)
	return err
}

func TestWindowsMethodsReturnTheSCMsError(t *testing.T) {
	running := scmStatus{State: scmRunning, ProcessID: 7}
	stopped := scmStatus{State: scmStopped}
	tests := []struct {
		name string
		call windowsCall
		// st is the status of the installed service; nil installs none.
		st *scmStatus
		// env is Config.Env.
		env   map[string]string
		fail  string
		after int
		// step is what the error names besides the injected error.
		step string
	}{
		{"Install create", (*windowsDriver).Install, nil, nil, "create", 0, "install demo: create:"},
		{"Install open", (*windowsDriver).Install, &running, nil, "open", 0, "install demo: open:"},
		{"Install config", (*windowsDriver).Install, &running, nil, "config", 0, "install demo: read config:"},
		{"Install updateConfig", (*windowsDriver).Install, &running, nil, "updateConfig", 0, "install demo: update config:"},
		{"Install setRecoveryActions", (*windowsDriver).Install, &running, nil, "setRecoveryActions", 0, "install demo: set recovery actions:"},
		{"Install setRecoveryOnNonCrashFailures", (*windowsDriver).Install, &running, nil, "setRecoveryOnNonCrashFailures", 0, "install demo: set recovery on non-crash failures:"},
		{"Install deleteEnvironment", (*windowsDriver).Install, &running, nil, "deleteEnvironment", 0, "install demo: delete environment:"},
		{"Install setEnvironment", (*windowsDriver).Install, &running, map[string]string{"A": "1"}, "setEnvironment", 0, "install demo: set environment:"},
		{"Install query", (*windowsDriver).Install, &running, nil, "query", 0, "previous run did not stop: query:"},
		{"Install stop", (*windowsDriver).Install, &running, nil, "stop", 0, "previous run did not stop: stop:"},
		{"Install start", (*windowsDriver).Install, &stopped, nil, "start", 0, "install demo: the service is configured but did not start:"},
		{"Uninstall open", (*windowsDriver).Uninstall, &running, nil, "open", 0, "open demo:"},
		{"Uninstall query", (*windowsDriver).Uninstall, &running, nil, "query", 0, "uninstall demo: query:"},
		{"Uninstall stop", (*windowsDriver).Uninstall, &running, nil, "stop", 0, "uninstall demo: stop:"},
		{"Uninstall delete", (*windowsDriver).Uninstall, &stopped, nil, "delete", 0, "uninstall demo: delete:"},
		{"Uninstall close", (*windowsDriver).Uninstall, &stopped, nil, "close", 0, "uninstall demo: close:"},
		{"Uninstall open after delete", (*windowsDriver).Uninstall, &stopped, nil, "open", 1, "uninstall demo: open:"},
		{"Start open", (*windowsDriver).Start, &stopped, nil, "open", 0, "open demo:"},
		{"Start start", (*windowsDriver).Start, &stopped, nil, "start", 0, "start demo:"},
		{"Stop open", (*windowsDriver).Stop, &running, nil, "open", 0, "open demo:"},
		{"Stop query", (*windowsDriver).Stop, &running, nil, "query", 0, "stop demo: query:"},
		{"Stop query while stopping", (*windowsDriver).Stop, &running, nil, "query", 1, "stop demo: query:"},
		{"Status open", windowsStatusCall, &running, nil, "open", 0, "status demo: service: open demo:"},
		{"Status config", windowsStatusCall, &running, nil, "config", 0, "status demo: read config:"},
		{"Status query", windowsStatusCall, &running, nil, "query", 0, "status demo: query:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := demoWindowsConfig()
			cfg.Env = tt.env
			w, f, _ := newTestWindows(cfg)
			if tt.st != nil {
				installed(f, *tt.st)
			}
			f.failOn(tt.fail, tt.after, errInjected)
			err := tt.call(w, t.Context())
			if !errors.Is(err, errInjected) {
				t.Fatalf("%s = %v, want the injected error", tt.name, err)
			}
			if !strings.Contains(err.Error(), tt.step) {
				t.Errorf("%s = %q, want it to name %q", tt.name, err, tt.step)
			}
			if errors.Is(err, ErrNotInstalled) {
				t.Errorf("%s = %v, which matches ErrNotInstalled", tt.name, err)
			}
			if f.openHandles != 0 {
				t.Errorf("%d service handles left open", f.openHandles)
			}
		})
	}
}

func TestWindowsStatusOnAnSCMErrorIsUnknown(t *testing.T) {
	for _, call := range []string{"open", "config", "query"} {
		t.Run(call, func(t *testing.T) {
			w, f, _ := newTestWindows(demoWindowsConfig())
			installed(f, scmStatus{State: scmRunning, ProcessID: 7})
			f.failOn(call, 0, errInjected)
			st, err := w.Status(t.Context())
			if !errors.Is(err, errInjected) || st != (Status{State: StateUnknown}) {
				t.Errorf("Status = %+v, %v, want StateUnknown and the injected error", st, err)
			}
		})
	}
}

func TestWindowsStopInsideTheRestartDelayReturnsAQueryError(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, restartCodeStop())
	f.failOn("query", 1, errInjected)
	err := w.Stop(t.Context())
	if !errors.Is(err, errInjected) || !strings.Contains(err.Error(), "stop demo: query:") {
		t.Fatalf("Stop = %v, want the second query's error", err)
	}
	if n := countCalls(f.calls, "query"); n != 2 {
		t.Errorf("queried %d times, want 2: the first look and the one inside the restart delay", n)
	}
}

func TestWindowsControlledMethodsNeedTheSCM(t *testing.T) {
	methods := map[string]windowsCall{
		"Uninstall": (*windowsDriver).Uninstall,
		"Start":     (*windowsDriver).Start,
		"Stop":      (*windowsDriver).Stop,
	}
	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			w, f, _ := newTestWindows(demoWindowsConfig())
			installed(f, scmStatus{State: scmRunning, ProcessID: 7})
			f.connectErr = errInjected
			err := call(w, t.Context())
			if !errors.Is(err, ErrManagerUnreachable) || !errors.Is(err, errInjected) {
				t.Fatalf("%s = %v, want ErrManagerUnreachable wrapping the connect error", name, err)
			}
			if !slices.Equal(f.calls, []string{"elevated", "connect control"}) {
				t.Errorf("calls = %q, want the elevation check and the connection only", f.calls)
			}
		})
	}
}

func TestWindowsFailingCloseOfTheManagerIsIgnored(t *testing.T) {
	w, f, _ := newTestWindows(demoWindowsConfig())
	installed(f, scmStatus{State: scmStopped})
	f.failOn("close manager", 0, errInjected)
	if err := w.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !slices.Contains(f.calls, "close manager") {
		t.Errorf("calls = %q, want the manager closed", f.calls)
	}
}

func TestWindowsInstallTakesTheContextsEndWhileWaitingOutADeletion(t *testing.T) {
	w, f, sleeps := newTestWindows(demoWindowsConfig())
	f.markedForDelete = -1
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := w.Install(ctx)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "install demo") {
		t.Fatalf("Install = %v, want context.Canceled", err)
	}
	if *sleeps != 1 || countCalls(f.calls, "create demo") != 1 {
		t.Errorf("slept %d times over calls %q, want one create and one sleep", *sleeps, f.calls)
	}
}

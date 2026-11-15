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
}

func (f *fakeSCM) record(call string) { f.calls = append(f.calls, call) }

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
	return nil
}

type fakeService struct{ f *fakeSCM }

func (s fakeService) config() (scmConfig, error) {
	s.f.record("config")
	return s.f.cfg, nil
}

func (s fakeService) updateConfig(cfg scmConfig) error {
	s.f.record("updateConfig")
	s.f.cfg = cfg
	return nil
}

func (s fakeService) setRecoveryActions(actions []scmRecoveryAction, resetPeriod uint32) error {
	s.f.record("setRecoveryActions")
	s.f.actions = slices.Clone(actions)
	s.f.resetPeriod = resetPeriod
	return nil
}

func (s fakeService) setRecoveryOnNonCrashFailures(on bool) error {
	s.f.record("setRecoveryOnNonCrashFailures")
	s.f.nonCrash = on
	return nil
}

func (s fakeService) setEnvironment(entries []string) error {
	s.f.record("setEnvironment " + strings.Join(entries, ","))
	s.f.env = slices.Clone(entries)
	return nil
}

func (s fakeService) deleteEnvironment() error {
	s.f.record("deleteEnvironment")
	s.f.env = nil
	return nil
}

func (s fakeService) start() error {
	s.f.record("start")
	if s.f.st.State == scmRunning {
		return errSCMServiceAlreadyRunning
	}
	s.f.st = scmStatus{State: scmRunning, ProcessID: 4242}
	return nil
}

func (s fakeService) stop() (scmStatus, error) {
	f := s.f
	f.record("stop")
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
	s.f.deleted = true
	return nil
}

func (s fakeService) close() error {
	s.f.record("close")
	s.f.openHandles--
	return nil
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
		want   State
		pid    int
		detail string
	}{
		{"running", scmStatus{State: scmRunning, ProcessID: 4242}, StateRunning, 4242, ""},
		{"start pending", scmStatus{State: scmStartPending, ProcessID: 9}, StateStarting, 9, ""},
		{"stop pending", scmStatus{State: scmStopPending}, StateStopped, 0, ""},
		{"paused", scmStatus{State: scmPaused, ProcessID: 3}, StateRunning, 3, ""},
		{"clean stop", scmStatus{State: scmStopped}, StateStopped, 0, ""},
		{"never started", scmStatus{State: scmStopped, Win32ExitCode: uint32(errSCMServiceNeverStarted)}, StateStopped, 0, ""},
		{"specific 0", scmStatus{State: scmStopped, Win32ExitCode: specific}, StateStopped, 0, ""},
		{"restart code", scmStatus{State: scmStopped, Win32ExitCode: specific, ServiceSpecificExitCode: daemon.WindowsRestartExitCode}, StateStarting, 0, ""},
		{"failure code", scmStatus{State: scmStopped, Win32ExitCode: specific, ServiceSpecificExitCode: daemon.WindowsFailureExitCode}, StateFailed, 0, "service-specific exit code 1"},
		{"win32 code", scmStatus{State: scmStopped, Win32ExitCode: 1067}, StateFailed, 0, "Win32 exit code 1067"},
		{"unknown state", scmStatus{State: 99}, StateUnknown, 0, "state 99"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, f, _ := newTestWindows(demoWindowsConfig())
			f.elevated = false
			installed(f, tt.st)
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

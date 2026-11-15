package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// windowsRestartExitCode is the service-specific exit code a service
	// reports with SERVICE_STOPPED when its Runner requested a restart. It
	// equals the restart exit code of the daemon package, whose handler
	// reports it; the SCM answers it with the recovery restart, and Status
	// reads it as StateStarting (ADR-0034).
	windowsRestartExitCode uint32 = 75

	// windowsRestartDelay is the delay of the one recovery action Install
	// sets, systemd's RestartSec and launchd's ThrottleInterval.
	windowsRestartDelay = 5 * time.Second
	// windowsResetPeriod is the time without a failure, in seconds, after
	// which the SCM resets the service's failure count. With a single
	// restart action every failure restarts the service whatever the count;
	// the period only bounds the count the SCM reports.
	windowsResetPeriod = uint32(24 * time.Hour / time.Second)

	// windowsWaitTimeout bounds each wait of the driver on the SCM: for a
	// service marked for deletion to disappear before it is created again,
	// for a stopping service to reach SERVICE_STOPPED, and for a deleted
	// service to disappear. windowsPollInterval is the wait between two
	// looks.
	windowsWaitTimeout  = 30 * time.Second
	windowsPollInterval = 250 * time.Millisecond

	// windowsLocalSystem is the account a service runs as when Config.User
	// is empty.
	windowsLocalSystem = "LocalSystem"
)

// scmErrno is a Win32 error code an scm implementation returns, wrapped, for
// every failure the SCM or the registry reports with one, so that the driver
// tells the codes apart with errors.Is on any host.
type scmErrno uint32

// Win32 error codes the driver tells apart.
const (
	errSCMAccessDenied           scmErrno = 5    // ERROR_ACCESS_DENIED
	errSCMServiceAlreadyRunning  scmErrno = 1056 // ERROR_SERVICE_ALREADY_RUNNING
	errSCMServiceDoesNotExist    scmErrno = 1060 // ERROR_SERVICE_DOES_NOT_EXIST
	errSCMServiceNotActive       scmErrno = 1062 // ERROR_SERVICE_NOT_ACTIVE
	errSCMServiceSpecificError   scmErrno = 1066 // ERROR_SERVICE_SPECIFIC_ERROR
	errSCMServiceMarkedForDelete scmErrno = 1072 // ERROR_SERVICE_MARKED_FOR_DELETE
	errSCMServiceNeverStarted    scmErrno = 1077 // ERROR_SERVICE_NEVER_STARTED
)

func (e scmErrno) Error() string {
	switch e {
	case errSCMAccessDenied:
		return "Access is denied."
	case errSCMServiceAlreadyRunning:
		return "An instance of the service is already running."
	case errSCMServiceDoesNotExist:
		return "The specified service does not exist as an installed service."
	case errSCMServiceNotActive:
		return "The service has not been started."
	case errSCMServiceMarkedForDelete:
		return "The specified service has been marked for deletion."
	}
	return "Win32 error " + strconv.FormatUint(uint64(e), 10)
}

// scmState is the CurrentState of a SERVICE_STATUS, with the values of
// svc.State in golang.org/x/sys/windows/svc.
type scmState uint32

const (
	scmStopped         scmState = 1 // SERVICE_STOPPED
	scmStartPending    scmState = 2 // SERVICE_START_PENDING
	scmStopPending     scmState = 3 // SERVICE_STOP_PENDING
	scmRunning         scmState = 4 // SERVICE_RUNNING
	scmContinuePending scmState = 5 // SERVICE_CONTINUE_PENDING
	scmPausePending    scmState = 6 // SERVICE_PAUSE_PENDING
	scmPaused          scmState = 7 // SERVICE_PAUSED
)

// Values of scmConfig fields the driver writes, as golang.org/x/sys/windows
// defines them.
const (
	scmServiceWin32OwnProcess uint32 = 0x10 // SERVICE_WIN32_OWN_PROCESS
	scmStartAutomatic         uint32 = 2    // SERVICE_AUTO_START
	scmErrorNormal            uint32 = 1    // SERVICE_ERROR_NORMAL
)

// scmActionRestart is the SC_ACTION_RESTART type of a recovery action, the
// mgr.ServiceRestart of golang.org/x/sys/windows/svc/mgr.
const scmActionRestart = 1

// scmAccess is the access a connection to the SCM asks for.
type scmAccess int

const (
	// scmQuery connects with SC_MANAGER_CONNECT and opens services with
	// SERVICE_QUERY_CONFIG and SERVICE_QUERY_STATUS, which an account
	// without elevation holds.
	scmQuery scmAccess = iota
	// scmControl connects with SC_MANAGER_ALL_ACCESS and opens services with
	// SERVICE_ALL_ACCESS, as mgr.Connect and mgr.Mgr.OpenService do.
	scmControl
)

// scmConfig is the configuration of a service, the fields of mgr.Config of
// golang.org/x/sys/windows/svc/mgr less Password, which the SCM never reveals
// and the driver never sets. updateConfig writes every field, so a caller
// passes back what config read, changed where it means to.
type scmConfig struct {
	ServiceType  uint32
	StartType    uint32
	ErrorControl uint32
	// BinaryPathName is the complete command line, each word already quoted
	// by windowsEscapeArg.
	BinaryPathName   string
	LoadOrderGroup   string
	TagID            uint32
	Dependencies     []string
	ServiceStartName string
	DisplayName      string
	Description      string
	SidType          uint32
	DelayedAutoStart bool
}

// scmRecoveryAction is one failure action of a service, mgr.RecoveryAction
// of golang.org/x/sys/windows/svc/mgr.
type scmRecoveryAction struct {
	// Type is scmActionRestart or another SC_ACTION type.
	Type int
	// Delay is the time the SCM waits before taking the action.
	Delay time.Duration
}

// scmStatus is the SERVICE_STATUS_PROCESS of a service, the fields of
// svc.Status of golang.org/x/sys/windows/svc that the driver reads.
type scmStatus struct {
	State     scmState
	ProcessID uint32
	// Win32ExitCode is ERROR_SERVICE_SPECIFIC_ERROR when the service reported
	// ServiceSpecificExitCode, ERROR_SERVICE_NEVER_STARTED for a service not
	// started since boot, and 0 after a clean stop.
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
}

// scm is the Windows service control manager as the driver sees it, a thin
// mapping of golang.org/x/sys/windows/svc/mgr and the service's registry key.
// Every error that carries a Win32 error code wraps it as an scmErrno.
type scm interface {
	// elevated reports whether the process runs with administrator rights,
	// as windows.GetCurrentProcessToken().IsElevated does.
	elevated() (bool, error)
	// connect opens the SCM of the local machine with access.
	connect(access scmAccess) (scmManager, error)
}

// scmManager is an open connection to the SCM, mgr.Mgr.
type scmManager interface {
	// open opens the service called name with the connection's access. A
	// service the SCM does not hold fails with errSCMServiceDoesNotExist.
	open(name string) (scmService, error)
	// create registers the service called name with cfg and returns it open.
	// cfg.BinaryPathName reaches CreateService unchanged: it is the quoted
	// command line, not an executable path for mgr.Mgr.CreateService, which
	// would quote it again. A name still held by a service marked for
	// deletion fails with errSCMServiceMarkedForDelete.
	create(name string, cfg scmConfig) (scmService, error)
	// close disconnects from the SCM.
	close() error
}

// scmService is an open service, mgr.Service.
type scmService interface {
	// config reads the service's configuration, mgr.Service.Config.
	config() (scmConfig, error)
	// updateConfig writes every field of cfg, mgr.Service.UpdateConfig. An
	// empty BinaryPathName, ServiceStartName or DisplayName leaves that field
	// as it is.
	updateConfig(cfg scmConfig) error
	// setRecoveryActions sets the failure actions and the period in seconds
	// after which the failure count resets, mgr.Service.SetRecoveryActions.
	setRecoveryActions(actions []scmRecoveryAction, resetPeriod uint32) error
	// setRecoveryOnNonCrashFailures sets whether a stop the service reports
	// with a non-zero exit code runs the failure actions,
	// mgr.Service.SetRecoveryActionsOnNonCrashFailures.
	setRecoveryOnNonCrashFailures(on bool) error
	// setEnvironment writes entries, each KEY=value, as the REG_MULTI_SZ
	// value Environment of the key
	// HKLM\SYSTEM\CurrentControlSet\Services\<name>, whose variables the SCM
	// adds to the service process's environment.
	setEnvironment(entries []string) error
	// deleteEnvironment deletes that Environment value. A value that does
	// not exist is not an error.
	deleteEnvironment() error
	// start starts the service, mgr.Service.Start with no arguments. A
	// running service fails with errSCMServiceAlreadyRunning.
	start() error
	// stop sends the stop control and returns the status the service
	// reports in answer, mgr.Service.Control(svc.Stop); it does not wait for
	// the service to stop. A stopped service fails with
	// errSCMServiceNotActive.
	stop() (scmStatus, error)
	// query reads the service's status, mgr.Service.Query.
	query() (scmStatus, error)
	// delete marks the service for deletion, mgr.Service.Delete. The SCM
	// removes it once its process has stopped and every handle to it is
	// closed.
	delete() error
	// close closes the handle to the service.
	close() error
}

// windowsDriver is the Manager of a Windows host, a ScopeSystem service of
// the service control manager named Config.Name. It reaches the SCM only
// through its scm field, so tests substitute a fake and the driver compiles
// and runs its tests on every host.
type windowsDriver struct {
	cfg Config
	// scm is the service control manager.
	scm scm
	// sleep waits d, or returns ctx's error when ctx ends first.
	sleep func(ctx context.Context, d time.Duration) error
}

// windowsEscapeArg quotes s as one word of a Windows command line, by the
// rules of syscall.EscapeArg, which CommandLineToArgvW reverses: an empty s
// is "", an s without '"', '\\', ' ' or '\t' is unchanged, a '"' is escaped
// with '\\' after the backslashes before it are doubled, and an s holding a
// space or a tab is wrapped in quotes, its trailing backslashes doubled.
func windowsEscapeArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, "\"\\ \t") {
		return s
	}
	hasSpace := strings.ContainsAny(s, " \t")
	if !strings.ContainsAny(s, `"\`) {
		return `"` + s + `"`
	}
	var b strings.Builder
	if hasSpace {
		b.WriteByte('"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes+1))
			slashes = 0
		default:
			slashes = 0
		}
		b.WriteByte(c)
	}
	if hasSpace {
		b.WriteString(strings.Repeat(`\`, slashes))
		b.WriteByte('"')
	}
	return b.String()
}

// windowsCommandLine is the command line the SCM starts: Binary and every
// Arg, each quoted by windowsEscapeArg, joined by spaces.
func windowsCommandLine(cfg Config) string {
	words := make([]string, 0, 1+len(cfg.Args))
	words = append(words, windowsEscapeArg(cfg.Binary))
	for _, a := range cfg.Args {
		words = append(words, windowsEscapeArg(a))
	}
	return strings.Join(words, " ")
}

// windowsEnvironment is Env as the entries of the Environment value, each
// KEY=value, sorted by key.
func windowsEnvironment(env map[string]string) []string {
	entries := make([]string, 0, len(env))
	for k, v := range env {
		entries = append(entries, k+"="+v)
	}
	slices.Sort(entries)
	return entries
}

// account is the account the service runs as, written explicitly so that a
// re-install moves a service off an account Config no longer names.
func (w *windowsDriver) account() string {
	if w.cfg.User == "" {
		return windowsLocalSystem
	}
	return w.cfg.User
}

// apply sets the fields of c that Config decides and returns c: the type,
// automatic start, normal error control, the command line, the account, the
// display name and the description. Every other field keeps the value c
// holds.
func (w *windowsDriver) apply(c scmConfig) scmConfig {
	c.ServiceType = scmServiceWin32OwnProcess
	c.StartType = scmStartAutomatic
	c.ErrorControl = scmErrorNormal
	c.BinaryPathName = windowsCommandLine(w.cfg)
	c.ServiceStartName = w.account()
	c.DisplayName = w.cfg.Name
	c.Description = w.cfg.Description
	return c
}

// requirePrivileges fails with an error wrapping ErrPrivilegesRequired when
// the process is not elevated.
func (w *windowsDriver) requirePrivileges(method string) error {
	ok, err := w.scm.elevated()
	if err != nil {
		return fmt.Errorf("service: windows %s of the service %s: check elevation: %w", method, w.cfg.Name, err)
	}
	if !ok {
		return fmt.Errorf("%w: windows %s of the service %s needs administrator rights, run it from an elevated prompt",
			ErrPrivilegesRequired, method, w.cfg.Name)
	}
	return nil
}

// connect opens the SCM with access. A failure wraps ErrManagerUnreachable.
func (w *windowsDriver) connect(access scmAccess) (scmManager, error) {
	m, err := w.scm.connect(access)
	if err != nil {
		return nil, fmt.Errorf("%w: connect to the service control manager: %w", ErrManagerUnreachable, err)
	}
	return m, nil
}

// open opens the service. A service the SCM does not hold fails with an
// error wrapping ErrNotInstalled.
func (w *windowsDriver) open(m scmManager) (scmService, error) {
	s, err := m.open(w.cfg.Name)
	if err != nil {
		if errors.Is(err, errSCMServiceDoesNotExist) {
			return nil, fmt.Errorf("%w: the service control manager holds no service %s: %w", ErrNotInstalled, w.cfg.Name, err)
		}
		return nil, fmt.Errorf("service: open %s: %w", w.cfg.Name, err)
	}
	return s, nil
}

// controlled checks privileges for method, connects with scmControl and opens
// the service, and calls fn with it. The connection is closed when fn
// returns, and so is the service unless fn has closed it through the close
// function it is given, which closes it at most once.
func (w *windowsDriver) controlled(method string, fn func(m scmManager, s scmService, close func() error) error) error {
	if err := w.requirePrivileges(method); err != nil {
		return err
	}
	m, err := w.connect(scmControl)
	if err != nil {
		return err
	}
	defer func() { _ = m.close() }()
	s, err := w.open(m)
	if err != nil {
		return err
	}
	closed := false
	closeOnce := func() error {
		if closed {
			return nil
		}
		closed = true
		return s.close()
	}
	defer func() { _ = closeOnce() }()
	return fn(m, s, closeOnce)
}

// poll calls done every windowsPollInterval until it reports true or fails,
// and fails once windowsWaitTimeout has passed; what names the awaited
// condition in that error.
func (w *windowsDriver) poll(ctx context.Context, what string, done func() (bool, error)) error {
	for waited := time.Duration(0); ; waited += windowsPollInterval {
		ok, err := done()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if waited >= windowsWaitTimeout {
			return fmt.Errorf("service: %s: %s after %v", w.cfg.Name, what, windowsWaitTimeout)
		}
		if err := w.sleep(ctx, windowsPollInterval); err != nil {
			return err
		}
	}
}

// Prepare checks privileges. The SCM keeps a service's whole registration,
// so there is nothing to create before Install.
func (w *windowsDriver) Prepare(context.Context) error {
	return w.requirePrivileges("Prepare")
}

// Install registers the service, or rewrites the configuration of the one
// already registered, then sets its recovery and its environment and starts
// it, stopping a running copy first so that the new configuration takes
// effect. The service starts automatically at boot, as Config.User or as
// LocalSystem when that is empty. Its one recovery action restarts it 5
// seconds after a failure, a stop with a non-zero exit code included
// (ADR-0034). Env is written to the service's Environment value, which is
// deleted when Env is empty. A name held by a service marked for deletion is
// waited out for up to windowsWaitTimeout.
func (w *windowsDriver) Install(ctx context.Context) error {
	if err := w.requirePrivileges("Install"); err != nil {
		return err
	}
	m, err := w.connect(scmControl)
	if err != nil {
		return err
	}
	defer func() { _ = m.close() }()

	var s scmService
	err = w.poll(ctx, "still marked for deletion", func() (bool, error) {
		var err error
		s, err = w.register(m)
		if errors.Is(err, errSCMServiceMarkedForDelete) {
			return false, nil
		}
		return err == nil, err
	})
	if err != nil {
		return fmt.Errorf("service: install %s: %w", w.cfg.Name, err)
	}
	defer func() { _ = s.close() }()

	if err := w.configure(s); err != nil {
		return fmt.Errorf("service: install %s: %w", w.cfg.Name, err)
	}
	if err := w.stopAndWait(ctx, s); err != nil {
		return fmt.Errorf("service: install %s: the service is configured but its previous run did not stop: %w",
			w.cfg.Name, err)
	}
	if err := s.start(); err != nil && !errors.Is(err, errSCMServiceAlreadyRunning) {
		return fmt.Errorf("service: install %s: the service is configured but did not start: %w", w.cfg.Name, err)
	}
	return nil
}

// register opens the service and rewrites its configuration through
// read-modify-write, or creates it when the SCM holds none. The returned
// service is open; on an error nothing is.
func (w *windowsDriver) register(m scmManager) (scmService, error) {
	s, err := m.open(w.cfg.Name)
	if errors.Is(err, errSCMServiceDoesNotExist) {
		s, err := m.create(w.cfg.Name, w.apply(scmConfig{}))
		if err != nil {
			return nil, fmt.Errorf("create: %w", err)
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	c, err := s.config()
	if err != nil {
		_ = s.close()
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := s.updateConfig(w.apply(c)); err != nil {
		_ = s.close()
		return nil, fmt.Errorf("update config: %w", err)
	}
	return s, nil
}

// configure sets the recovery action, the non-crash-failures flag and the
// Environment value of s.
func (w *windowsDriver) configure(s scmService) error {
	actions := []scmRecoveryAction{{Type: scmActionRestart, Delay: windowsRestartDelay}}
	if err := s.setRecoveryActions(actions, windowsResetPeriod); err != nil {
		return fmt.Errorf("set recovery actions: %w", err)
	}
	if err := s.setRecoveryOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("set recovery on non-crash failures: %w", err)
	}
	if len(w.cfg.Env) == 0 {
		if err := s.deleteEnvironment(); err != nil {
			return fmt.Errorf("delete environment: %w", err)
		}
		return nil
	}
	if err := s.setEnvironment(windowsEnvironment(w.cfg.Env)); err != nil {
		return fmt.Errorf("set environment: %w", err)
	}
	return nil
}

// stopAndWait sends the stop control to s unless it is stopped, and waits
// until it reports SERVICE_STOPPED.
func (w *windowsDriver) stopAndWait(ctx context.Context, s scmService) error {
	st, err := s.query()
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	if st.State == scmStopped {
		return nil
	}
	if st.State != scmStopPending {
		if _, err := s.stop(); err != nil && !errors.Is(err, errSCMServiceNotActive) {
			return fmt.Errorf("stop: %w", err)
		}
	}
	return w.poll(ctx, "still not stopped", func() (bool, error) {
		st, err := s.query()
		if err != nil {
			return false, fmt.Errorf("query: %w", err)
		}
		return st.State == scmStopped, nil
	})
}

// Uninstall stops the service, deletes it and returns once the SCM no longer
// holds it, waiting up to windowsWaitTimeout. The registry key, its
// Environment value included, goes with the service. The service's data and
// its account stay.
func (w *windowsDriver) Uninstall(ctx context.Context) error {
	return w.controlled("Uninstall", func(m scmManager, s scmService, closeService func() error) error {
		if err := w.stopAndWait(ctx, s); err != nil {
			return fmt.Errorf("service: uninstall %s: %w", w.cfg.Name, err)
		}
		if err := s.delete(); err != nil && !errors.Is(err, errSCMServiceMarkedForDelete) {
			return fmt.Errorf("service: uninstall %s: delete: %w", w.cfg.Name, err)
		}
		// The SCM removes a deleted service only once every handle to it is
		// closed, this one included.
		if err := closeService(); err != nil {
			return fmt.Errorf("service: uninstall %s: close: %w", w.cfg.Name, err)
		}
		err := w.poll(ctx, "still registered after delete", func() (bool, error) {
			s, err := m.open(w.cfg.Name)
			if errors.Is(err, errSCMServiceDoesNotExist) {
				return true, nil
			}
			if err != nil {
				return false, fmt.Errorf("open: %w", err)
			}
			_ = s.close()
			return false, nil
		})
		if err != nil {
			return fmt.Errorf("service: uninstall %s: %w", w.cfg.Name, err)
		}
		return nil
	})
}

// Start starts the installed service. A running service keeps running.
func (w *windowsDriver) Start(context.Context) error {
	return w.controlled("Start", func(_ scmManager, s scmService, _ func() error) error {
		if err := s.start(); err != nil && !errors.Is(err, errSCMServiceAlreadyRunning) {
			return fmt.Errorf("service: start %s: %w", w.cfg.Name, err)
		}
		return nil
	})
}

// Stop stops the installed service and waits, up to windowsWaitTimeout,
// until it reports SERVICE_STOPPED. Stopping a stopped service succeeds. A
// service an operator stops exits 0 and is not restarted by its recovery
// action.
func (w *windowsDriver) Stop(ctx context.Context) error {
	return w.controlled("Stop", func(_ scmManager, s scmService, _ func() error) error {
		if err := w.stopAndWait(ctx, s); err != nil {
			return fmt.Errorf("service: stop %s: %w", w.cfg.Name, err)
		}
		return nil
	})
}

// Status reports the service as the SCM describes it. It needs no
// elevation. A service the SCM does not hold is StateNotInstalled. A service
// stopped with windowsRestartExitCode is inside its recovery delay and
// reported as StateStarting; one stopped with any other non-zero exit code is
// StateFailed. Enabled reports automatic start, and Restarts is always 0: the
// SCM keeps no restart count.
func (w *windowsDriver) Status(context.Context) (Status, error) {
	m, err := w.connect(scmQuery)
	if err != nil {
		return Status{State: StateUnknown}, fmt.Errorf("service: status %s: %w", w.cfg.Name, err)
	}
	defer func() { _ = m.close() }()
	s, err := w.open(m)
	if err != nil {
		if errors.Is(err, ErrNotInstalled) {
			return Status{State: StateNotInstalled}, nil
		}
		return Status{State: StateUnknown}, fmt.Errorf("service: status %s: %w", w.cfg.Name, err)
	}
	defer func() { _ = s.close() }()
	c, err := s.config()
	if err != nil {
		return Status{State: StateUnknown}, fmt.Errorf("service: status %s: read config: %w", w.cfg.Name, err)
	}
	q, err := s.query()
	if err != nil {
		return Status{State: StateUnknown}, fmt.Errorf("service: status %s: query: %w", w.cfg.Name, err)
	}
	st := w.status(q)
	st.Enabled = c.StartType == scmStartAutomatic
	return st, nil
}

// status maps the SCM's status of the service to a Status.
func (w *windowsDriver) status(q scmStatus) Status {
	st := Status{State: StateUnknown}
	switch q.State {
	case scmRunning, scmContinuePending, scmPausePending, scmPaused:
		st.State = StateRunning
		st.PID = int(q.ProcessID)
	case scmStartPending:
		st.State = StateStarting
		st.PID = int(q.ProcessID)
	case scmStopPending:
		st.State = StateStopped
	case scmStopped:
		st.State, st.Detail = w.stopped(q)
	default:
		st.Detail = fmt.Sprintf("the service control manager reports state %d for %s", q.State, w.cfg.Name)
	}
	st.State = resolve(st.State, 0)
	return st
}

// stopped maps the exit codes of a stopped service to its State and Detail.
func (w *windowsDriver) stopped(q scmStatus) (State, string) {
	switch scmErrno(q.Win32ExitCode) {
	case 0, errSCMServiceNeverStarted:
		return StateStopped, ""
	case errSCMServiceSpecificError:
		switch q.ServiceSpecificExitCode {
		case 0:
			return StateStopped, ""
		case windowsRestartExitCode:
			return StateStarting, ""
		}
		return StateFailed, fmt.Sprintf("%s stopped with service-specific exit code %d", w.cfg.Name, q.ServiceSpecificExitCode)
	}
	return StateFailed, fmt.Sprintf("%s stopped with Win32 exit code %d: %v", w.cfg.Name, q.Win32ExitCode, scmErrno(q.Win32ExitCode))
}

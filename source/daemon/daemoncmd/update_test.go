package daemoncmd_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon/daemoncmd"
	"go.vipengele.org/daemon/daemontest"
	"go.vipengele.org/daemon/selfupdate"
	"go.vipengele.org/daemon/service"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatetest"
)

// The tests that run the update command never call t.Parallel: the command runs the staged
// release binary, and a process forked by another test inherits every descriptor open at that
// moment until it execs, so an executable some parallel test still holds open for writing fails
// the exec with ETXTBSY.

const (
	updateName  = "atk"
	updateOwner = "pedromvgomes"
	updateRepo  = "agentic-toolkit"
	fromVersion = "v1.0.0"
	toVersion   = "v1.1.0"
	stays       = "stays down once it exits"
)

// updateFixture is an executable holding "old" in its own resolved directory, an updatetest
// server publishing release toVersion of it, a Config running fromVersion whose executable is
// that file and whose requests go to the server, and a Machine over a file store.
type updateFixture struct {
	exe     string
	srv     *updatetest.Server
	cfg     update.Config
	store   *selfupdate.FileStore
	machine *selfupdate.Machine
	clock   *daemontest.Clock
}

func newUpdateFixture(t *testing.T) updateFixture {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	exe := filepath.Join(dir, updateName)
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rel := updatetest.NewRelease(t, updateOwner, updateRepo, toVersion, updateName)
	srv := updatetest.NewServer(t, rel)
	home := t.TempDir()

	cfg := update.Config{
		Identity: spec.Identity{
			Name:      updateName,
			Owner:     updateOwner,
			Repo:      updateRepo,
			EnvPrefix: "ATK",
			DirName:   updateRepo,
		},
		Version:     fromVersion,
		TrustedKeys: []string{srv.PublicKey()},
		TempDir:     t.TempDir(),
		StateDir:    t.TempDir(),
		HTTPClient:  srv.Client(),
		Executable:  func() (string, error) { return exe, nil },
		InContainer: func() bool { return false },
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		Source: &update.GitHubSource{
			Owner:      updateOwner,
			Repo:       updateRepo,
			HTTPClient: srv.Client(),
			WebURL:     srv.WebURL(),
			APIURL:     srv.APIURL(),
		},
	}

	store := selfupdate.NewFileStore(filepath.Join(t.TempDir(), "update.json"))
	m, err := selfupdate.New(store, fromVersion)
	if err != nil {
		t.Fatalf("selfupdate.New: %v", err)
	}

	return updateFixture{
		exe:     exe,
		srv:     srv,
		cfg:     cfg,
		store:   store,
		machine: m,
		clock:   daemontest.NewClock(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)),
	}
}

// config is the UpdateConfig of f acting on mgr for a service running f's executable.
func (f updateFixture) config(mgr daemoncmd.ManagerFunc) daemoncmd.UpdateConfig {
	return daemoncmd.UpdateConfig{
		Update:        f.cfg,
		Machine:       f.machine,
		Manager:       mgr,
		ServiceBinary: f.exe,
		Clock:         f.clock,
	}
}

// downloads returns how many requests the server has received for the release's assets.
func (f updateFixture) downloads() int {
	return f.srv.Requests(updatetest.TargetArchive) + f.srv.Requests(updatetest.TargetChecksums) +
		f.srv.Requests(updatetest.TargetSignature)
}

// assertInstalled fails t unless the executable holds the release of toVersion.
func (f updateFixture) assertInstalled(t *testing.T) {
	t.Helper()
	got, err := os.ReadFile(f.exe)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, updatetest.ReleaseExecutable(t, toVersion)) {
		t.Errorf("executable holds %q, want the release of %s", got, toVersion)
	}
}

// assertOld fails t unless the file at path holds the previous binary.
func assertOld(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "old" {
		t.Errorf("%s holds %q, want %q", path, got, "old")
	}
}

// sequenceManager is a fakeManager whose Status returns states in turn, repeating the last one.
type sequenceManager struct {
	*fakeManager
	states []service.State
}

func (s *sequenceManager) Status(context.Context) (service.Status, error) {
	st := service.Status{State: s.states[0]}
	if len(s.states) > 1 {
		s.states = s.states[1:]
	}
	return st, s.call("Status")
}

// managerFunc returns a ManagerFunc that records its call and returns s.
func (s *sequenceManager) managerFunc() daemoncmd.ManagerFunc {
	return func(context.Context, *spec.Invocation) (service.Manager, error) {
		return s, s.call("manager")
	}
}

// newSequence returns a sequenceManager reporting states in turn, failing the methods errs names.
func newSequence(errs map[string]error, states ...service.State) *sequenceManager {
	return &sequenceManager{fakeManager: &fakeManager{errs: errs}, states: states}
}

// result is what one run of the update command wrote and returned.
type result struct {
	stdout, stderr string
	err            error
}

// runUpdate runs the update command of cfg with --yes, advancing clock by the poll interval
// whenever the command waits on it, until the command returns.
func runUpdate(t *testing.T, cfg daemoncmd.UpdateConfig, clock *daemontest.Clock) result {
	t.Helper()

	cmds := daemoncmd.NewUpdate(cfg)
	if len(cmds) != 1 {
		t.Fatalf("NewUpdate returned %d commands, want 1", len(cmds))
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan result, 1)
	go func() {
		defer cancel()
		stdout, stderr, err := runOutput(t, cmds[0], spec.OutputOptions{}, map[string]any{"yes": true})
		done <- result{stdout: stdout, stderr: stderr, err: err}
	}()

	for clock.BlockUntilTimers(ctx, 1) == nil {
		clock.Advance(500 * time.Millisecond)
	}
	return <-done
}

// assertContains fails t unless s holds every one of want.
func assertContains(t *testing.T, what, s string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("%s = %q, want it to contain %q", what, s, w)
		}
	}
}

// assertCalls fails t unless the manager's calls are want.
func assertCalls(t *testing.T, f *fakeManager, want ...string) {
	t.Helper()
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

// assertPending fails t unless the record is the pending update of f's executable.
func assertPending(t *testing.T, f updateFixture) {
	t.Helper()
	rec, err := f.machine.State(t.Context())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if rec.Status != selfupdate.StatusPending || rec.FromVersion != fromVersion || rec.ToVersion != toVersion ||
		rec.Target != f.exe || rec.Backup != f.exe+".old" {
		t.Errorf("record = %+v, want pending %s → %s of %s", rec, fromVersion, toVersion, f.exe)
	}
}

func TestNewUpdateIsAValidCommand(t *testing.T) {
	f := newUpdateFixture(t)
	cmds := daemoncmd.NewUpdate(f.config(newSequence(nil, service.StateRunning).managerFunc()))
	if len(cmds) != 1 {
		t.Fatalf("NewUpdate returned %d commands, want 1", len(cmds))
	}
	if err := cmds[0].Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if cmds[0].Name != "update" {
		t.Errorf("Name = %q, want update", cmds[0].Name)
	}
}

func TestNewUpdatePanicsWithoutAMachineOrAManager(t *testing.T) {
	f := newUpdateFixture(t)
	for name, cfg := range map[string]daemoncmd.UpdateConfig{
		"Machine": {Manager: newSequence(nil, service.StateRunning).managerFunc()},
		"Manager": {Machine: f.machine},
	} {
		func() {
			defer func() {
				r := recover()
				if msg, _ := r.(string); !strings.Contains(msg, name+" is nil") {
					t.Errorf("panic = %v, want one naming %s", r, name)
				}
			}()
			daemoncmd.NewUpdate(cfg)
		}()
	}
}

func TestUpdateRestartsARunningServiceAndWaitsUntilItRuns(t *testing.T) {
	f := newUpdateFixture(t)
	mgr := newSequence(nil, service.StateRunning, service.StateStarting, service.StateStarting, service.StateRunning)

	r := runUpdate(t, f.config(mgr.managerFunc()), f.clock)
	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	assertCalls(t, mgr.fakeManager, "manager", "Status", "Restart", "Status", "Status", "Status")
	assertContains(t, "stderr", r.stderr, "restarted and runs "+toVersion, "updated "+updateName)
	f.assertInstalled(t)
	assertPending(t, f)
}

func TestUpdateKeepsTheBackupOfASuccessfulUpdate(t *testing.T) {
	f := newUpdateFixture(t)
	mgr := newSequence(nil, service.StateRunning)

	if r := runUpdate(t, f.config(mgr.managerFunc()), f.clock); r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	f.assertInstalled(t)
	assertOld(t, f.exe+".old")
}

func TestUpdateFailsWhenTheRestartedServiceDoesNotRun(t *testing.T) {
	tests := map[string]struct {
		mgr   *sequenceManager
		state string
		calls []string
	}{
		"restart error": {
			mgr:   newSequence(map[string]error{"Restart": errors.New("systemctl: exit 1")}, service.StateRunning),
			state: "while it was running: systemctl: exit 1",
			calls: []string{"manager", "Status", "Restart"},
		},
		"failed": {
			mgr:   newSequence(nil, service.StateRunning, service.StateStarting, service.StateFailed),
			state: "it is failed after the restart",
			calls: []string{"manager", "Status", "Restart", "Status", "Status"},
		},
		"not installed": {
			mgr:   newSequence(nil, service.StateRunning, service.StateNotInstalled),
			state: "it is not_installed after the restart",
			calls: []string{"manager", "Status", "Restart", "Status"},
		},
		"timeout": {
			mgr:   newSequence(nil, service.StateRunning, service.StateStarting),
			state: "it is still starting 15s after the restart",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newUpdateFixture(t)

			r := runUpdate(t, f.config(tc.mgr.managerFunc()), f.clock)
			if r.err == nil {
				t.Fatal("Run error = nil, want one")
			}
			assertContains(t, "error", r.err.Error(), toVersion+" is installed", tc.state, "boot-attempt limit")
			if strings.Contains(r.stderr, "updated "+updateName) {
				t.Errorf("stderr = %q, want no success line", r.stderr)
			}
			if tc.calls != nil {
				assertCalls(t, tc.mgr.fakeManager, tc.calls...)
			}
			if n := slices.Index(tc.mgr.calls, "Restart"); n < 0 || slices.Index(tc.mgr.calls[n+1:], "Restart") >= 0 {
				t.Errorf("calls = %v, want one Restart", tc.mgr.calls)
			}
			f.assertInstalled(t)
			assertPending(t, f)
		})
	}
}

// failsAfterRestart is a fakeManager whose Status reports a running service until Restart is
// called and fails with err from then on.
type failsAfterRestart struct {
	*fakeManager
	err error
}

func (s *failsAfterRestart) Status(context.Context) (service.Status, error) {
	restarted := slices.Contains(s.calls, "Restart")
	s.calls = append(s.calls, "Status")
	if restarted {
		return service.Status{State: service.StateRunning, Detail: "stale"}, s.err
	}
	return service.Status{State: service.StateRunning}, nil
}

// managerFunc returns a ManagerFunc that records its call and returns s.
func (s *failsAfterRestart) managerFunc() daemoncmd.ManagerFunc {
	return func(context.Context, *spec.Invocation) (service.Manager, error) {
		return s, s.call("manager")
	}
}

func TestUpdateFailsWhenTheStatusCannotBeReadAfterTheRestart(t *testing.T) {
	f := newUpdateFixture(t)
	errRead := errors.New("systemctl show: exit 1")
	mgr := &failsAfterRestart{fakeManager: &fakeManager{}, err: errRead}
	cfg := f.config(mgr.managerFunc())
	cfg.StartTimeout = time.Second

	r := runUpdate(t, cfg, f.clock)
	if !errors.Is(r.err, errRead) {
		t.Fatalf("Run error = %v, want one matching the Status error", r.err)
	}
	assertContains(t, "error", r.err.Error(), toVersion+" is installed",
		"it is still unknown ("+errRead.Error()+") 1s after the restart")
	if strings.Contains(r.err.Error(), "stale") {
		t.Errorf("error = %q, want no detail of the status returned beside the error", r.err)
	}
	assertCalls(t, mgr.fakeManager, "manager", "Status", "Restart", "Status", "Status", "Status")
	f.assertInstalled(t)
	assertPending(t, f)
}

func TestUpdateFailsWhenTheWaitIsCancelled(t *testing.T) {
	f := newUpdateFixture(t)
	mgr := newSequence(nil, service.StateRunning, service.StateStarting)
	cmds := daemoncmd.NewUpdate(f.config(mgr.managerFunc()))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout, stderr bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdout: &stdout, Stderr: &stderr}, nil, map[string]any{"yes": true})
	inv.Output = spec.NewOutput(spec.OutputOptions{NoColorStdout: true, NoColorStderr: true})
	done := make(chan error, 1)
	go func() { done <- cmds[0].Run(ctx, inv) }()

	if err := f.clock.BlockUntilTimers(t.Context(), 1); err != nil {
		t.Fatalf("BlockUntilTimers: %v", err)
	}
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want one matching context.Canceled", err)
	}
	assertContains(t, "error", err.Error(), toVersion+" is installed", "the wait for it ended while it was starting")
	assertCalls(t, mgr.fakeManager, "manager", "Status", "Restart", "Status")
	f.assertInstalled(t)
	assertPending(t, f)
}

func TestUpdateTimesOutAfterTheStartTimeout(t *testing.T) {
	f := newUpdateFixture(t)
	mgr := newSequence(nil, service.StateRunning, service.StateStarting)
	cfg := f.config(mgr.managerFunc())
	cfg.StartTimeout = 2 * time.Second
	start := f.clock.Now()

	r := runUpdate(t, cfg, f.clock)
	if r.err == nil {
		t.Fatal("Run error = nil, want one")
	}
	assertContains(t, "error", r.err.Error(), "still starting 2s after the restart")
	if waited := f.clock.Now().Sub(start); waited != 2*time.Second {
		t.Errorf("waited %s, want 2s", waited)
	}
}

func TestUpdateLeavesAServiceThatDoesNotRunAlone(t *testing.T) {
	tests := map[string]struct {
		state service.State
		want  string
	}{
		"stopped":  {state: service.StateStopped, want: "runs " + toVersion + " when next started"},
		"starting": {state: service.StateStarting, want: "the next launch runs the new binary"},
		"failed":   {state: service.StateFailed, want: "the service is failed; start it by hand"},
		"unknown":  {state: service.StateUnknown, want: "state of the service is unknown; restart it yourself"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newUpdateFixture(t)
			mgr := newSequence(nil, tc.state)

			r := runUpdate(t, f.config(mgr.managerFunc()), f.clock)
			if r.err != nil {
				t.Fatalf("Run error = %v, want nil", r.err)
			}
			assertCalls(t, mgr.fakeManager, "manager", "Status")
			assertContains(t, "stderr", r.stderr, tc.want, "updated "+updateName)
			f.assertInstalled(t)
			assertPending(t, f)
		})
	}
}

func TestUpdateWarnsWhenTheStatusCannotBeRead(t *testing.T) {
	f := newUpdateFixture(t)
	mgr := newSequence(map[string]error{"Status": service.ErrManagerUnreachable}, service.StateUnknown)

	r := runUpdate(t, f.config(mgr.managerFunc()), f.clock)
	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	assertCalls(t, mgr.fakeManager, "manager", "Status")
	assertContains(t, "stderr", r.stderr, "unknown ("+service.ErrManagerUnreachable.Error()+")")
}

func TestUpdateSaysNothingRestartsAnUnmanagedBinary(t *testing.T) {
	tests := map[string]struct {
		mgr    daemoncmd.ManagerFunc
		binary string
	}{
		"not installed": {mgr: newSequence(nil, service.StateNotInstalled).managerFunc()},
		"manager says not installed": {
			mgr: failing(service.ErrNotInstalled),
		},
		"container": {mgr: failing(&service.ContainerError{Instead: "restart the container"})},
		"unsupported": {
			mgr: failing(service.ErrUnsupported),
		},
		"status unsupported": {
			mgr: newSequence(map[string]error{"Status": service.ErrUnsupported}, service.StateUnknown).managerFunc(),
		},
		"another binary": {
			mgr:    newSequence(nil, service.StateRunning).managerFunc(),
			binary: filepath.Join(t.TempDir(), updateName),
		},
		"no binary": {
			mgr:    newSequence(nil, service.StateRunning).managerFunc(),
			binary: "-",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newUpdateFixture(t)
			cfg := f.config(tc.mgr)
			switch tc.binary {
			case "":
			case "-":
				cfg.ServiceBinary = ""
			default:
				cfg.ServiceBinary = tc.binary
			}

			r := runUpdate(t, cfg, f.clock)
			if r.err != nil {
				t.Fatalf("Run error = %v, want nil", r.err)
			}
			assertContains(t, "stderr", r.stderr, "no service manager runs "+f.exe, stays, "updated "+updateName)
			f.assertInstalled(t)
			assertPending(t, f)
		})
	}
}

func TestUpdateRestartsNothingForAnotherBinary(t *testing.T) {
	f := newUpdateFixture(t)
	mgr := newSequence(nil, service.StateRunning)
	cfg := f.config(mgr.managerFunc())
	cfg.ServiceBinary = filepath.Join(t.TempDir(), updateName)

	if r := runUpdate(t, cfg, f.clock); r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	assertCalls(t, mgr.fakeManager)
}

func TestUpdateRestartsAServiceWhoseBinaryIsTheInstalledOneByARelativePath(t *testing.T) {
	f := newUpdateFixture(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	rel, err := filepath.Rel(wd, f.exe)
	if err != nil {
		t.Skipf("no relative path from %s to %s: %v", wd, f.exe, err)
	}
	mgr := newSequence(nil, service.StateRunning)
	cfg := f.config(mgr.managerFunc())
	cfg.ServiceBinary = rel

	if r := runUpdate(t, cfg, f.clock); r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	assertCalls(t, mgr.fakeManager, "manager", "Status", "Restart", "Status")
}

func TestUpdateComparesTheServiceBinaryWithoutRegardToCaseOnlyOnWindows(t *testing.T) {
	f := newUpdateFixture(t)
	mgr := newSequence(nil, service.StateRunning)
	cfg := f.config(mgr.managerFunc())
	cfg.ServiceBinary = filepath.Join(filepath.Dir(f.exe), strings.ToUpper(updateName))

	r := runUpdate(t, cfg, f.clock)
	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if runtime.GOOS == "windows" {
		assertCalls(t, mgr.fakeManager, "manager", "Status", "Restart", "Status")
		return
	}
	assertCalls(t, mgr.fakeManager)
	assertContains(t, "stderr", r.stderr, "no service manager runs "+f.exe)
}

func TestUpdateWarnsWhenTheManagerFuncFails(t *testing.T) {
	f := newUpdateFixture(t)

	r := runUpdate(t, f.config(failing(service.ErrInvalidConfig)), f.clock)
	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	assertContains(t, "stderr", r.stderr, service.ErrInvalidConfig.Error(), "restart it yourself to run "+toVersion)
}

func TestUpdateRefusesWhileAnUpdateIsPending(t *testing.T) {
	f := newUpdateFixture(t)
	if err := f.machine.Begin(t.Context(), selfupdate.Pending{
		FromVersion: "v0.9.0", ToVersion: fromVersion, Target: f.exe, Backup: f.exe + ".old",
	}); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	mgr := newSequence(nil, service.StateRunning)

	r := runUpdate(t, f.config(mgr.managerFunc()), f.clock)
	if !errors.Is(r.err, update.ErrBackupInUse) {
		t.Fatalf("Run error = %v, want one matching update.ErrBackupInUse", r.err)
	}
	if n := f.downloads(); n != 0 {
		t.Errorf("downloads = %d, want 0", n)
	}
	assertCalls(t, mgr.fakeManager)
	assertOld(t, f.exe)
}

// beginFails is a selfupdate.Store that holds no update and fails every Update, running before
// first.
type beginFails struct {
	before func()
}

var errStore = errors.New("store unavailable")

func (beginFails) Load(context.Context) (selfupdate.Record, error) { return selfupdate.Record{}, nil }

func (s beginFails) Update(context.Context, func(selfupdate.Record) (selfupdate.Record, error)) error {
	if s.before != nil {
		s.before()
	}
	return errStore
}

func TestUpdateRollsBackWhenBeginFails(t *testing.T) {
	f := newUpdateFixture(t)
	m, err := selfupdate.New(beginFails{}, fromVersion)
	if err != nil {
		t.Fatalf("selfupdate.New: %v", err)
	}
	mgr := newSequence(nil, service.StateRunning)
	cfg := f.config(mgr.managerFunc())
	cfg.Machine = m

	r := runUpdate(t, cfg, f.clock)
	if !errors.Is(r.err, errStore) {
		t.Fatalf("Run error = %v, want one matching the store's", r.err)
	}
	assertContains(t, "error", r.err.Error(), "rolled back")
	assertOld(t, f.exe)
	assertCalls(t, mgr.fakeManager)
}

func TestUpdateNamesTheBackupWhenTheRollbackFailsToo(t *testing.T) {
	f := newUpdateFixture(t)
	backup := f.exe + ".old"
	m, err := selfupdate.New(beginFails{before: func() { _ = os.Remove(backup) }}, fromVersion)
	if err != nil {
		t.Fatalf("selfupdate.New: %v", err)
	}
	mgr := newSequence(nil, service.StateRunning)
	cfg := f.config(mgr.managerFunc())
	cfg.Machine = m

	r := runUpdate(t, cfg, f.clock)
	if !errors.Is(r.err, errStore) {
		t.Fatalf("Run error = %v, want one matching the store's", r.err)
	}
	assertContains(t, "error", r.err.Error(), errStore.Error(), "rolling it back failed too", backup)
	assertCalls(t, mgr.fakeManager)
}

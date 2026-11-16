package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"go.vipengele.org/daemon"
)

const (
	// e2ePoll is the interval between two looks at the service.
	e2ePoll = 100 * time.Millisecond
	// e2eWait bounds every wait for the service to reach a state.
	e2eWait = 60 * time.Second
	// e2eQuiet is how long the service is watched for a recovery restart
	// after a Stop inside its restart delay returns: more than twice the
	// delay.
	e2eQuiet = 2*windowsRestartDelay + 2*time.Second
)

// e2eService is a service the end-to-end test installs from
// testdata/winsvc, with the files it shares with that program.
type e2eService struct {
	t   *testing.T
	m   Manager
	cfg Config
	// marker is the file whose appearance asks winsvc for a restart.
	marker string
}

// TestWindowsServiceEndToEnd installs testdata/winsvc as a service of the
// host's service control manager and drives it through install, re-install,
// a requested restart, a Stop inside the restart delay, Restart, Stop and
// Uninstall.
// It needs an elevated process: it skips without one, and fails under CI,
// whose Windows runners are elevated.
func TestWindowsServiceEndToEnd(t *testing.T) {
	requireElevation(t)
	dir := e2eDir(t)
	bin := buildWinsvc(t, dir)
	logPath := filepath.Join(dir, "winsvc.log")
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Logf("winsvc log: %v", err)
			return
		}
		t.Logf("winsvc log:\n%s", data)
	})

	cfg := Config{
		Name:        "vipengele-daemon-e2e-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		Description: "End-to-end test service of go.vipengele.org/daemon/service",
		Binary:      bin,
		Env: map[string]string{
			"WINSVC_MARKER": filepath.Join(dir, "restart.marker"),
			"WINSVC_LOG":    logPath,
		},
	}
	e := newE2EService(t, cfg)
	t.Cleanup(e.remove)
	ctx := t.Context()

	if st := e.status(); st.State != StateNotInstalled {
		t.Fatalf("Status before Install = %+v, want %v", st, StateNotInstalled)
	}

	// Install registers and starts the service.
	if err := e.m.Install(ctx); err != nil {
		t.Fatalf("Install: %v", err)
	}
	first := e.waitRunning("running after Install", 0)
	if !first.Enabled {
		t.Errorf("Status after Install = %+v, want Enabled, the service starting at boot", first)
	}

	// A re-install with another binary rewrites the command line and
	// restarts the service from the new binary.
	update := cfg
	update.Binary = filepath.Join(dir, "winsvc-update.exe")
	copyFile(t, bin, update.Binary)
	e = newE2EService(t, update)
	if err := e.m.Install(ctx); err != nil {
		t.Fatalf("re-Install: %v", err)
	}
	updated := e.waitRunning("running after the re-Install", first.PID)
	if got, want := e.config().BinaryPathName, windowsCommandLine(update); got != want {
		t.Errorf("command line after the re-Install = %q, want %q", got, want)
	}

	// A requested restart stops the service with the restart code, which
	// Status reads as starting, and the recovery action starts it again.
	e.requestRestart()
	seen := map[State]bool{}
	restarted := e.waitStatus("running again with a new PID after a restart request", func(st Status) bool {
		seen[st.State] = true
		if st.State != StateRunning && st.State != StateStarting {
			t.Fatalf("Status during a requested restart = %+v, want only %v or %v", st, StateStarting, StateRunning)
		}
		return st.State == StateRunning && st.PID > 0 && st.PID != updated.PID
	})
	if !seen[StateStarting] {
		t.Errorf("Status during a requested restart went from PID %d to PID %d without reporting %v",
			updated.PID, restarted.PID, StateStarting)
	}

	// A Stop inside the restart delay outlasts it and stops the service the
	// recovery restart starts, so the service stays stopped after Stop
	// returns and Status reads its exit code 0 as stopped.
	e.requestRestart()
	e.waitRestartDelay()
	if st := e.status(); st.State != StateStarting {
		t.Errorf("Status inside the restart delay = %+v, want %v", st, StateStarting)
	}
	if err := e.m.Stop(ctx); err != nil {
		t.Fatalf("Stop inside the restart delay: %v", err)
	}
	if st := e.status(); st.State != StateStopped || st.PID != 0 {
		t.Errorf("Status after Stop inside the restart delay = %+v, want %v with no PID", st, StateStopped)
	}
	e.stayStopped(e2eQuiet)

	// Restart replaces a running service's process with a new one.
	if err := e.m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	started := e.waitRunning("running after Start", 0)
	if err := e.m.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	e.waitRunning("running with a new PID after Restart", started.PID)

	// Stop stops a running service with exit code 0, which Status reads as
	// stopped.
	if err := e.m.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if st := e.status(); st.State != StateStopped || st.PID != 0 {
		t.Fatalf("Status after Stop = %+v, want %v with no PID", st, StateStopped)
	}

	// Uninstall returns once the service is gone, so an Install right after
	// it registers the name again.
	if err := e.m.Uninstall(ctx); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if st := e.status(); st.State != StateNotInstalled {
		t.Fatalf("Status after Uninstall = %+v, want %v", st, StateNotInstalled)
	}
	if err := e.m.Install(ctx); err != nil {
		t.Fatalf("Install right after Uninstall: %v", err)
	}
	if err := e.m.Uninstall(ctx); err != nil {
		t.Fatalf("Uninstall right after Install: %v", err)
	}
	if st := e.status(); st.State != StateNotInstalled {
		t.Fatalf("Status after the second Uninstall = %+v, want %v", st, StateNotInstalled)
	}
}

// requireElevation skips the test in a process without administrator
// rights, and fails it there under CI, whose Windows runners are elevated.
func requireElevation(t *testing.T) {
	t.Helper()
	if windows.GetCurrentProcessToken().IsElevated() {
		return
	}
	if os.Getenv("CI") != "" {
		t.Fatal("the end-to-end test of the Windows service driver needs an elevated process, and CI runs elevated")
	}
	t.Skip("the end-to-end test of the Windows service driver needs an elevated process")
}

// e2eDir returns a new directory for the service's binary and files. Its
// removal at the end of the test is best effort: the SCM may still hold the
// binary of a service process that has just exited, which would fail the
// removal t.TempDir makes.
func e2eDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "winsvc-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var err error
		for range 20 {
			if err = os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Logf("remove %s: %v", dir, err)
	})
	return dir
}

// buildWinsvc builds testdata/winsvc into dir and returns the executable's
// absolute path.
func buildWinsvc(t *testing.T, dir string) string {
	t.Helper()
	exe := filepath.Join(dir, "winsvc.exe")
	out, err := exec.Command("go", "build", "-o", exe, "./testdata/winsvc").CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./testdata/winsvc: %v\n%s", err, out)
	}
	return exe
}

// copyFile copies the executable src to dst.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o700); err != nil {
		t.Fatal(err)
	}
}

// newE2EService returns the service of cfg, managed through New on the host.
func newE2EService(t *testing.T, cfg Config) *e2eService {
	t.Helper()
	m, err := New(cfg, notInContainer())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &e2eService{t: t, m: m, cfg: cfg, marker: cfg.Env["WINSVC_MARKER"]}
}

// remove stops and uninstalls the service, whatever state the test left it
// in. A service already gone is not an error.
func (e *e2eService) remove() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := e.m.Stop(ctx); err != nil && !errors.Is(err, ErrNotInstalled) {
		e.t.Logf("cleanup: Stop: %v", err)
	}
	if err := e.m.Uninstall(ctx); err != nil && !errors.Is(err, ErrNotInstalled) {
		e.t.Logf("cleanup: Uninstall: %v", err)
	}
}

// status is the service's Status, which must not fail.
func (e *e2eService) status() Status {
	e.t.Helper()
	st, err := e.m.Status(e.t.Context())
	if err != nil {
		e.t.Fatalf("Status: %v", err)
	}
	return st
}

// waitStatus polls Status until done reports true and returns that Status.
// It fails the test after e2eWait, naming what it waited for.
func (e *e2eService) waitStatus(what string, done func(Status) bool) Status {
	e.t.Helper()
	deadline := time.Now().Add(e2eWait)
	for {
		st := e.status()
		if done(st) {
			return st
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("service not %s after %v: last Status %+v", what, e2eWait, st)
		}
		time.Sleep(e2ePoll)
	}
}

// waitRunning waits until Status is running with a PID other than 0 and
// notPID, and returns that Status.
func (e *e2eService) waitRunning(what string, notPID int) Status {
	e.t.Helper()
	return e.waitStatus(what, func(st Status) bool {
		return st.State == StateRunning && st.PID > 0 && st.PID != notPID
	})
}

// requestRestart creates the marker file, which winsvc answers with
// ErrRestartRequired.
func (e *e2eService) requestRestart() {
	e.t.Helper()
	if err := os.WriteFile(e.marker, nil, 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// query reads the status of the service from the SCM, below Status.
func (e *e2eService) query() scmStatus {
	e.t.Helper()
	m, err := hostSCM().connect(scmQuery)
	if err != nil {
		e.t.Fatalf("connect: %v", err)
	}
	defer func() { _ = m.close() }()
	s, err := m.open(e.cfg.Name)
	if err != nil {
		e.t.Fatalf("open %s: %v", e.cfg.Name, err)
	}
	defer func() { _ = s.close() }()
	q, err := s.query()
	if err != nil {
		e.t.Fatalf("query %s: %v", e.cfg.Name, err)
	}
	return q
}

// config reads the configuration of the service from the SCM.
func (e *e2eService) config() scmConfig {
	e.t.Helper()
	m, err := hostSCM().connect(scmQuery)
	if err != nil {
		e.t.Fatalf("connect: %v", err)
	}
	defer func() { _ = m.close() }()
	s, err := m.open(e.cfg.Name)
	if err != nil {
		e.t.Fatalf("open %s: %v", e.cfg.Name, err)
	}
	defer func() { _ = s.close() }()
	c, err := s.config()
	if err != nil {
		e.t.Fatalf("config %s: %v", e.cfg.Name, err)
	}
	return c
}

// waitRestartDelay waits until the SCM reports the service stopped with the
// restart code, inside its restart delay.
func (e *e2eService) waitRestartDelay() {
	e.t.Helper()
	deadline := time.Now().Add(e2eWait)
	for {
		q := e.query()
		if inRestartDelay(q) {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("service not stopped with the restart code %d after %v: last SCM status %+v",
				daemon.WindowsRestartExitCode, e2eWait, q)
		}
		time.Sleep(e2ePoll)
	}
}

// stayStopped fails the test when the SCM reports the service in any state
// but stopped within d.
func (e *e2eService) stayStopped(d time.Duration) {
	e.t.Helper()
	start := time.Now()
	for time.Since(start) < d {
		if q := e.query(); q.State != scmStopped {
			e.t.Fatalf("the service ran again after Stop inside the restart delay returned: %v after Stop the SCM reports %+v, want it stopped",
				time.Since(start).Round(time.Millisecond), q)
		}
		time.Sleep(e2ePoll)
	}
}

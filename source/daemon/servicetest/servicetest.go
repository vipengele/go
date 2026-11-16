package servicetest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon/service"
	"go.vipengele.org/daemon/servicetest/internal/wire"
)

const (
	// enableEnv names the variable that must be "1" for any container to run.
	enableEnv = "SERVICETEST"
	// DefaultTimeout bounds each scenario when Target.Timeout is zero. The
	// unit restarts its service RestartSec=5 after it exits, and the kill and
	// crash-loop scenarios each wait for one such restart; a minute leaves
	// room for several.
	DefaultTimeout = time.Minute
	// setupTimeout bounds, each on its own, the driver build, the image build,
	// the boot of the container and the copy of the binaries into it.
	setupTimeout = 10 * time.Minute
	// driverPackage is the package of the driver the harness builds when
	// Target.Driver is empty.
	driverPackage = "go.vipengele.org/daemon/servicetest/driver"
	// driverPath is where the driver is copied in the container.
	driverPath = "/usr/local/bin/servicetest-driver"
	// crashBinary is the Binary of the crash-loop scenario: it exits 1 at
	// once, every time it is started.
	crashBinary = "/bin/false"
	// systemUnitDir holds the unit files of system units, and userUnitDir
	// those of userName's user units.
	systemUnitDir = "/etc/systemd/system"
	userUnitDir   = userHome + "/.config/systemd/user"
)

// Target is the service Run proves under systemd.
type Target struct {
	// Binary is the host path of the service's executable, built for Linux on
	// the host's architecture. Run copies it into the container at
	// Config.Binary.
	Binary string
	// Driver is the host path of the servicetest driver, built for Linux on
	// the host's architecture with
	//
	//	GOOS=linux CGO_ENABLED=0 go build go.vipengele.org/daemon/servicetest/driver
	//
	// Empty means Run builds it with the go command in the test's working
	// directory, which then has to be inside a module that requires
	// go.vipengele.org/daemon and does not vendor its dependencies: a vendor
	// directory holds only the packages the module imports, never the driver.
	Driver string
	// Config is the service as the consumer installs it. Its Scope is the
	// scope under test: ScopeSystem installs a system unit as root, ScopeUser
	// a user unit of the lingering account svc. Config.Binary is an absolute
	// path in the container.
	Config service.Config
	// Notify, when non-nil, installs the service with
	// service.WithSystemdNotify.
	Notify *Notify
	// Hardening, when non-nil, installs the service with
	// service.WithSystemdHardening.
	Hardening *service.Hardening
	// Identity, when non-nil, installs the service with service.WithIdentity.
	Identity *spec.Identity
	// BaseImage replaces the Debian base image the container is built from.
	// It must be a Debian-based image that installs systemd with apt-get.
	// Empty means the base pinned by digest in the embedded Containerfile.
	BaseImage string
	// Timeout bounds each scenario. Zero means DefaultTimeout.
	Timeout time.Duration
}

// Notify holds the input of service.WithSystemdNotify.
type Notify struct {
	// Watchdog is the WatchdogSec= interval; 0 writes no WatchdogSec= line.
	Watchdog time.Duration
}

// SkipUnlessEnabled skips tb unless the environment variable SERVICETEST is
// "1".
func SkipUnlessEnabled(tb testing.TB) {
	tb.Helper()
	skipUnless(tb, os.LookupEnv)
}

// skipUnless skips tb unless lookup reports SERVICETEST as "1".
func skipUnless(tb testing.TB, lookup func(string) (string, bool)) {
	tb.Helper()
	if v, _ := lookup(enableEnv); v != "1" {
		tb.Skip("servicetest: set " + enableEnv + "=1 to run the service in a podman container under systemd")
	}
}

// Run proves target in a podman container with systemd as PID 1, booted for
// target.Config.Scope, as a subtest of t named after the scope. It skips t
// unless SERVICETEST is "1", and fails it when podman is not usable, the
// driver cannot be built or the container does not boot.
//
// Run copies Target.Binary and the driver into the container and runs five
// scenarios in order, each a subtest, through the driver, which calls
// service.New with target's Config and options as the consumer would:
//
//   - install: Install, then the service reaches running, enabled, with a
//     main PID.
//   - kill: SIGKILL to the main PID, then systemd restarts the service under
//     a new main PID with more restarts than before.
//   - reinstall: Install again, then the service runs under a new main PID.
//   - crash_loop: Install of the same Config with Binary /bin/false, then
//     the service reports StateFailed with restarts and a Detail.
//   - uninstall: Uninstall, then the service reports StateNotInstalled and
//     its unit file is gone.
//
// Each scenario's state depends on the one before, so the first that fails
// ends the run and the later ones do not run; a failure logs the unit's
// systemctl status and journal. Every scenario has Target.Timeout. The
// container is removed when the scope's subtest ends.
func Run(t *testing.T, target Target) {
	t.Helper()
	SkipUnlessEnabled(t)
	run(t, newHarness(t, os.LookupEnv), target)
}

// run is Run on h, once SERVICETEST is checked.
func run(t *testing.T, h harness, target Target) {
	t.Helper()
	driver := setup(t, h, target)
	runScope(t, h, target.Config.Scope, target.BaseImage, func(t *testing.T, c *container) {
		newSuite(c, target).run(t, subtests(t), driver)
	})
}

// check reports the first field of target the harness cannot work with.
func (target Target) check() error {
	switch {
	case target.Binary == "":
		return errors.New("servicetest: Target.Binary is empty, want the host path of the service's executable")
	case !path.IsAbs(target.Config.Binary):
		return fmt.Errorf("servicetest: Target.Config.Binary %q is not an absolute path in the container", target.Config.Binary)
	}
	_, err := scopeName(target.Config.Scope)
	return err
}

// setup checks target and returns the host path of the driver, building it
// through h when target.Driver is empty. It fails tb when either does not
// succeed.
func setup(tb testing.TB, h harness, target Target) string {
	tb.Helper()
	if err := target.check(); err != nil {
		tb.Fatal(err)
	}
	if target.Driver != "" {
		return target.Driver
	}
	ctx, cancel := context.WithTimeout(tb.Context(), setupTimeout)
	defer cancel()
	driver, err := h.buildDriver(ctx, tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	return driver
}

// goBuildDriver builds the driver for Linux on the host's architecture into
// out with the go command, in the working directory, and returns the
// command's combined output.
func goBuildDriver(ctx context.Context, out string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, driverPackage) // #nosec G204 -- the program is the constant go; its arguments are the constant build verb and driver package and an output path under the test's temporary directory; no shell is involved
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	return cmd.CombinedOutput()
}

// buildDriver builds the driver into dir through h.goBuild and returns its
// path.
func (h harness) buildDriver(ctx context.Context, dir string) (string, error) {
	out := filepath.Join(dir, "servicetest-driver")
	if msg, err := h.goBuild(ctx, out); err != nil {
		return "", fmt.Errorf("servicetest: build the driver %s for linux/%s: %w: %s; "+
			"run the test inside a module that requires go.vipengele.org/daemon and does not vendor it, "+
			"or set Target.Driver to a driver built with `GOOS=linux CGO_ENABLED=0 go build %s`",
			driverPackage, runtime.GOARCH, err, bytes.TrimSpace(msg), driverPackage)
	}
	return out, nil
}

// plan holds what a run derives before its container starts.
type plan struct {
	image string
	name  string
}

// prepare checks scope, builds or reuses the image for base, and returns the
// plan; it fails tb when either does not succeed.
func prepare(tb testing.TB, h harness, scope service.Scope, base string) plan {
	tb.Helper()
	name, err := scopeName(scope)
	if err != nil {
		tb.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(tb.Context(), setupTimeout)
	defer cancel()
	image, err := h.ensureImage(ctx, base, tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	return plan{image: image, name: name}
}

// runScope boots a container for scope from the image for base in a subtest
// of t named after the scope, and calls body with the booted container.
func runScope(t *testing.T, h harness, scope service.Scope, base string, body func(*testing.T, *container)) {
	t.Helper()
	p := prepare(t, h, scope, base)
	t.Run(p.name, func(t *testing.T) {
		body(t, boot(t, h, p.image, scope, setupTimeout))
	})
}

// subtest runs body as a subtest named name and reports whether it passed.
type subtest func(name string, body func(testing.TB)) bool

// subtests returns the subtest that runs body under t.Run.
func subtests(t *testing.T) subtest {
	return func(name string, body func(testing.TB)) bool {
		return t.Run(name, func(t *testing.T) { body(t) })
	}
}

// suite drives one service through the scenarios in one booted container.
type suite struct {
	c       *container
	target  Target
	timeout time.Duration
	// user drives the service as userName, through its user manager.
	user bool
	unit string
	// pid and restarts are the main PID and the restart count the last
	// scenario left the running service with.
	pid      int
	restarts int
}

// newSuite returns the suite for target in c.
func newSuite(c *container, target Target) *suite {
	timeout := target.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &suite{
		c:       c,
		target:  target,
		timeout: timeout,
		user:    target.Config.Scope == service.ScopeUser,
		unit:    target.Config.Name + ".service",
	}
}

// scenario is one step of the suite.
type scenario struct {
	name string
	run  func(context.Context) error
}

// scenarios returns the scenarios in the order they run.
func (s *suite) scenarios() []scenario {
	return []scenario{
		{"install", s.install},
		{"kill", s.kill},
		{"reinstall", s.reinstall},
		{"crash_loop", s.crashLoop},
		{"uninstall", s.uninstall},
	}
}

// run copies the service's binary and the host's driver into the container,
// failing tb when that does not succeed, and then runs every scenario through
// sub until one fails.
func (s *suite) run(tb testing.TB, sub subtest, driver string) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), setupTimeout)
	err := s.place(ctx, driver)
	cancel()
	if err != nil {
		tb.Fatal(err)
	}
	for _, sc := range s.scenarios() {
		if !sub(sc.name, func(tb testing.TB) { s.step(tb, sc) }) {
			return
		}
	}
}

// step runs sc within the scenario timeout and fails tb, after logging the
// unit's diagnostics, when it does not succeed.
func (s *suite) step(tb testing.TB, sc scenario) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), s.timeout)
	defer cancel()
	if err := sc.run(ctx); err != nil {
		// ctx may be the one that ran out.
		dctx, dcancel := context.WithTimeout(context.Background(), removeTimeout)
		defer dcancel()
		s.c.logDiagnostics(dctx, tb, s.unit, s.user)
		tb.Fatal(err)
	}
}

// place copies the service's binary to Config.Binary, creating its
// directory, and driver to driverPath, and makes both executable by every
// account, userName's included.
func (s *suite) place(ctx context.Context, driver string) error {
	dst := s.target.Config.Binary
	if _, err := s.c.exec(ctx, execOptions{}, "mkdir", "-p", path.Dir(dst)); err != nil {
		return fmt.Errorf("servicetest: create %s: %w", path.Dir(dst), err)
	}
	if err := s.c.copyIn(ctx, s.target.Binary, dst); err != nil {
		return err
	}
	if err := s.c.copyIn(ctx, driver, driverPath); err != nil {
		return err
	}
	if _, err := s.c.exec(ctx, execOptions{}, "chmod", "0755", dst, driverPath); err != nil {
		return fmt.Errorf("servicetest: make %s and %s executable: %w", dst, driverPath, err)
	}
	return nil
}

// call runs the driver for op on cfg with the target's options, as root for
// a system unit and as userName for a user unit, and returns its response.
// It fails when the response is unreadable, carries an error, or the driver
// exits non-zero.
func (s *suite) call(ctx context.Context, op wire.Op, cfg service.Config) (wire.Response, error) {
	req := wire.Request{Op: op, Config: cfg, Hardening: s.target.Hardening, Identity: s.target.Identity}
	if n := s.target.Notify; n != nil {
		req.Notify = &wire.Notify{Watchdog: n.Watchdog}
	}
	// A Request holds only strings, booleans, integers and a map of strings,
	// which always encode.
	in, _ := json.Marshal(req)
	out, err := s.c.exec(ctx, execOptions{stdin: bytes.NewReader(in), user: s.user}, driverPath)
	var resp wire.Response
	if derr := json.Unmarshal(out, &resp); derr != nil {
		return resp, fmt.Errorf("servicetest: driver %s: unreadable response %q: %w", op, out, errors.Join(derr, err))
	}
	if rerr := resp.Err(); rerr != nil {
		return resp, fmt.Errorf("servicetest: driver %s: %w", op, rerr)
	}
	if err != nil {
		return resp, fmt.Errorf("servicetest: driver %s: %w", op, err)
	}
	return resp, nil
}

// status returns the service's status as the driver reports it.
func (s *suite) status(ctx context.Context) (service.Status, error) {
	resp, err := s.call(ctx, wire.OpStatus, s.target.Config)
	if err != nil {
		return service.Status{}, err
	}
	if resp.Status == nil {
		return service.Status{}, errors.New("servicetest: driver status: the response carries no status")
	}
	return *resp.Status, nil
}

// await reads the service's status, pausing pollInterval between two reads,
// until want accepts it, and returns that status. When ctx ends first it
// fails with an error that says the unit what, with the last status and
// error read.
func (s *suite) await(ctx context.Context, what string, want func(service.Status) bool) (service.Status, error) {
	for {
		st, err := s.status(ctx)
		if err == nil && want(st) {
			return st, nil
		}
		if serr := s.c.h.sleep(ctx, pollInterval); serr != nil {
			return st, fmt.Errorf("servicetest: %s %s; last status: %s: %w", s.unit, what, describe(st), errors.Join(serr, err))
		}
	}
}

// describe renders st for a failure message.
func describe(st service.Status) string {
	return fmt.Sprintf("state %s, enabled %t, pid %d, restarts %d, detail %q",
		st.State, st.Enabled, st.PID, st.Restarts, st.Detail)
}

// running reports whether st is a running service with a main PID other
// than old.
func running(st service.Status, old int) bool {
	return st.State == service.StateRunning && st.PID > 0 && st.PID != old
}

// install installs the service and waits until it runs, enabled.
func (s *suite) install(ctx context.Context) error {
	if _, err := s.call(ctx, wire.OpInstall, s.target.Config); err != nil {
		return err
	}
	st, err := s.await(ctx, "did not reach running and enabled with a main PID", func(st service.Status) bool {
		return running(st, 0) && st.Enabled
	})
	if err != nil {
		return err
	}
	s.pid, s.restarts = st.PID, st.Restarts
	return nil
}

// kill sends SIGKILL to the main PID, as root whatever the scope, and waits
// until systemd has restarted the service. The restart follows RestartSec=5
// after the kill. The container's base image has no kill program, so the
// shell's builtin sends the signal.
func (s *suite) kill(ctx context.Context) error {
	old, restarts := s.pid, s.restarts
	if _, err := s.c.exec(ctx, execOptions{}, "sh", "-c", `kill -9 "$1"`, "sh", strconv.Itoa(old)); err != nil {
		return fmt.Errorf("servicetest: kill -9 the main PID %d of %s: %w", old, s.unit, err)
	}
	st, err := s.await(ctx, fmt.Sprintf("was not restarted after kill -9 of its main PID %d", old), func(st service.Status) bool {
		return running(st, old) && st.Restarts > restarts
	})
	if err != nil {
		return err
	}
	s.pid, s.restarts = st.PID, st.Restarts
	return nil
}

// reinstall installs the service over itself and waits until it runs,
// enabled, under a new main PID: Install restarts a running service.
func (s *suite) reinstall(ctx context.Context) error {
	old := s.pid
	if _, err := s.call(ctx, wire.OpInstall, s.target.Config); err != nil {
		return err
	}
	st, err := s.await(ctx, fmt.Sprintf("did not run, enabled, under a main PID other than %d after reinstall", old),
		func(st service.Status) bool { return running(st, old) && st.Enabled })
	if err != nil {
		return err
	}
	s.pid, s.restarts = st.PID, st.Restarts
	return nil
}

// crashLoop installs the service's unit with crashBinary in place of its
// Binary and waits until the service reports StateFailed with restarts and a
// Detail.
//
// The unit sets Restart=always and RestartSec=5 and leaves systemd's start
// limit at its defaults, StartLimitBurst=5 within StartLimitIntervalSec=10s.
// Starts 5 seconds apart never reach 5 within 10 seconds, so the unit never
// hits the limit and never settles in ActiveState=failed; it cycles through
// activating (auto-restart) with Result=exit-code instead. The service
// package reports a unit activating after a failed run with restarts as
// StateFailed, which the first automatic restart, about 5 seconds after the
// install, makes true for most of each cycle.
//
// A Type=notify unit fails its start job when the process exits before
// READY=1, so Install may report that failure while the unit keeps
// restarting; its error is reported only when the service does not reach
// StateFailed.
func (s *suite) crashLoop(ctx context.Context) error {
	cfg := s.target.Config
	cfg.Binary = crashBinary
	_, installErr := s.call(ctx, wire.OpInstall, cfg)
	_, err := s.await(ctx, "did not report failed with restarts and a detail while its binary crashes", func(st service.Status) bool {
		return st.State == service.StateFailed && st.Restarts > 0 && st.Detail != ""
	})
	if err != nil {
		return errors.Join(err, installErr)
	}
	return nil
}

// uninstall uninstalls the service and checks that it reports
// StateNotInstalled and that its unit file is gone.
func (s *suite) uninstall(ctx context.Context) error {
	if _, err := s.call(ctx, wire.OpUninstall, s.target.Config); err != nil {
		return err
	}
	st, err := s.status(ctx)
	if err != nil {
		return err
	}
	if st.State != service.StateNotInstalled {
		return fmt.Errorf("servicetest: %s is still installed after uninstall: %s", s.unit, describe(st))
	}
	file := s.unitPath()
	if _, err := s.c.exec(ctx, execOptions{}, "test", "!", "-e", file); err != nil {
		return fmt.Errorf("servicetest: the unit file %s remains after uninstall: %w", file, err)
	}
	return nil
}

// unitPath is the path of the unit file in the container.
func (s *suite) unitPath() string {
	if s.user {
		return path.Join(userUnitDir, s.unit)
	}
	return path.Join(systemUnitDir, s.unit)
}

package servicetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/daemon/service"
)

const (
	// podmanEnv names the variable that selects how podman runs: rootless as
	// defaultPodman, or rootful as sudoPodman.
	podmanEnv = "SERVICETEST_PODMAN"
	// defaultPodman is the value of podmanEnv that runs podman as the current
	// user, and what an unset or blank variable means.
	defaultPodman = "podman"
	// sudoPodman is the value of podmanEnv that runs podman through sudo.
	sudoPodman = "sudo podman"
	// containerLabel labels every servicetest container, so a CI job can
	// remove the ones a killed run left behind.
	containerLabel = "servicetest"
	// userName and userHome are the account the Containerfile creates for
	// user-scope services, with lingering on.
	userName = "svc"
	userHome = "/home/svc"
	// pollInterval is the pause between two checks of a state that is not
	// reached yet.
	pollInterval = time.Second
	// removeTimeout bounds the removal of a container at cleanup, which runs
	// after the test's own context is cancelled.
	removeTimeout = 2 * time.Minute
	// diagnosticLines is how many journal or log lines a diagnostic shows.
	diagnosticLines = "200"
)

// runner runs one podman command. It is the seam between the container
// lifecycle and the podman executable: the plain suite drives the lifecycle
// with a fake.
type runner interface {
	// run runs podman with args, feeding it stdin when stdin is non-nil, and
	// returns its standard output. A failure returns whatever was written to
	// standard output with an error that carries the standard error.
	run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error)
}

// commandError is a podman command that failed.
type commandError struct {
	args   []string
	stderr string
	err    error
}

func (e *commandError) Error() string {
	msg := strings.Join(e.args, " ") + ": " + e.err.Error()
	if s := strings.TrimSpace(e.stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

func (e *commandError) Unwrap() error { return e.err }

// execRunner runs podman as a child process, through sudo when sudo is set.
type execRunner struct {
	sudo bool
}

// newExecRunner returns the runner for the value lookup reports for
// SERVICETEST_PODMAN. Only "podman", which an unset or blank variable also
// means, and "sudo podman" are accepted, so the program that runs is always a
// constant; any other value is an error.
func newExecRunner(lookup func(string) (string, bool)) (execRunner, error) {
	v, _ := lookup(podmanEnv)
	switch strings.Join(strings.Fields(v), " ") {
	case "", defaultPodman:
		return execRunner{}, nil
	case sudoPodman:
		return execRunner{sudo: true}, nil
	}
	return execRunner{}, fmt.Errorf("servicetest: %s is %q, want %q or %q", podmanEnv, v, defaultPodman, sudoPodman)
}

// commandLine returns the words every command starts with.
func (e execRunner) commandLine() []string {
	if e.sudo {
		return []string{"sudo", "podman"}
	}
	return []string{"podman"}
}

func (e execRunner) run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	var cmd *exec.Cmd
	if e.sudo {
		cmd = exec.CommandContext(ctx, "sudo", append([]string{"podman"}, args...)...) // #nosec G204 -- the program is the constant sudo running the constant podman; the arguments are the harness's own podman verbs and flags and container IDs and paths it generated, passed without a shell
	} else {
		cmd = exec.CommandContext(ctx, "podman", args...) // #nosec G204 -- the program is the constant podman; its arguments are the harness's own podman verbs and flags and container IDs and paths it generated, passed without a shell
	}
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &commandError{
			args:   append(e.commandLine(), args...),
			stderr: stderr.String(),
			err:    err,
		}
	}
	return stdout.Bytes(), nil
}

// harness drives podman through r, waits through sleep and builds the
// driver through goBuild, which builds it into the path it is given and
// returns the go command's output.
type harness struct {
	r       runner
	sleep   func(context.Context, time.Duration) error
	goBuild func(ctx context.Context, out string) ([]byte, error)
}

// newHarness returns the harness for the podman that lookup selects, and fails
// tb when the selection is invalid or that podman does not answer: a run that
// asked for the containers with SERVICETEST=1 and cannot get them is a
// failure, not a skip.
func newHarness(tb testing.TB, lookup func(string) (string, bool)) harness {
	tb.Helper()
	r, err := newExecRunner(lookup)
	if err != nil {
		tb.Fatal(err)
	}
	h := harness{r: r, sleep: sleepContext, goBuild: goBuildDriver}
	if err := h.checkPodman(tb.Context()); err != nil {
		tb.Fatal(err)
	}
	return h
}

// sleepContext waits for d, or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// checkPodman reports whether podman answers at all.
func (h harness) checkPodman(ctx context.Context) error {
	if _, err := h.r.run(ctx, nil, "version"); err != nil {
		return fmt.Errorf("servicetest: podman is not usable (%s selects podman or sudo podman): %w", podmanEnv, err)
	}
	return nil
}

// scopeName names scope in container labels and subtest names.
func scopeName(scope service.Scope) (string, error) {
	switch scope {
	case service.ScopeSystem:
		return "system", nil
	case service.ScopeUser:
		return "user", nil
	default:
		return "", fmt.Errorf("servicetest: scope %d is neither ScopeSystem nor ScopeUser", int(scope)) // [lydite:exclude_from_mutation][every caller discards the name when the error is set, so the name returned with it is unobservable]
	}
}

// container is one running servicetest container, booted with systemd as
// PID 1, for one scope.
type container struct {
	h     harness
	id    string
	scope service.Scope
	// uid is the uid of userName, set once the user manager is waited for.
	uid string
}

// start runs a detached, privileged container of image in systemd mode,
// labelled containerLabel, and returns it once podman reports its ID.
func (h harness) start(ctx context.Context, image string, scope service.Scope) (*container, error) {
	name, err := scopeName(scope)
	if err != nil {
		return nil, err
	}
	out, err := h.r.run(ctx, nil, "run", "--detach", "--privileged", "--systemd=always",
		"--label", containerLabel, "--label", containerLabel+".scope="+name, image)
	if err != nil {
		return nil, fmt.Errorf("servicetest: start the %s container: %w", name, err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" || strings.ContainsAny(id, " \t\n") {
		return nil, fmt.Errorf("servicetest: start the %s container: podman printed %q, not a container ID", name, out)
	}
	return &container{h: h, id: id, scope: scope}, nil
}

// remove removes the container, stopping it first.
func (c *container) remove(ctx context.Context) error {
	if _, err := c.h.r.run(ctx, nil, "rm", "--force", c.id); err != nil {
		return fmt.Errorf("servicetest: remove container %s: %w", c.id, err)
	}
	return nil
}

// execOptions are the settings of one command run in the container.
type execOptions struct {
	// stdin, when non-nil, is the command's standard input.
	stdin io.Reader
	// user runs the command as userName with the environment that reaches its
	// user manager, rather than as root.
	user bool
}

// exec runs argv in the container and returns its standard output.
func (c *container) exec(ctx context.Context, opts execOptions, argv ...string) ([]byte, error) {
	args := []string{"exec"}
	if opts.stdin != nil {
		args = append(args, "--interactive")
	}
	if opts.user {
		if c.uid == "" {
			return nil, errors.New("servicetest: exec as " + userName + " before its uid is known")
		}
		runtime := "/run/user/" + c.uid
		args = append(args, "--user", userName, "--workdir", userHome,
			"--env", "HOME="+userHome,
			"--env", "XDG_RUNTIME_DIR="+runtime,
			"--env", "DBUS_SESSION_BUS_ADDRESS=unix:path="+runtime+"/bus")
	}
	args = append(args, c.id)
	args = append(args, argv...)
	return c.h.r.run(ctx, opts.stdin, args...)
}

// copyIn copies the host file or directory src to dst in the container.
func (c *container) copyIn(ctx context.Context, src, dst string) error {
	if _, err := c.h.r.run(ctx, nil, "cp", src, c.id+":"+dst); err != nil {
		return fmt.Errorf("servicetest: copy %s into %s: %w", src, dst, err)
	}
	return nil
}

// systemctl returns the systemctl command line for the system manager, or
// for the user manager when user is true.
func systemctl(user bool, args ...string) []string {
	argv := []string{"systemctl"}
	if user {
		argv = append(argv, "--user")
	}
	return append(argv, args...)
}

// waitRunning waits until the system manager, or userName's manager when
// user is true, has finished booting, and returns its state.
//
// Both "running" and "degraded" count as booted. A degraded manager has some
// failed unit, and in a container that is often one the container cannot
// support; the scenarios check the state of their own unit, which a failed
// unrelated unit does not change, so they run on a degraded manager too, and
// boot logs which units failed.
func (c *container) waitRunning(ctx context.Context, user bool) (string, error) {
	for {
		// is-system-running exits non-zero for every state but "running", so
		// the state is read from its output whatever the error.
		out, err := c.exec(ctx, execOptions{user: user}, systemctl(user, "is-system-running", "--wait")...)
		state := strings.TrimSpace(string(out))
		switch state {
		case "running", "degraded":
			return state, nil
		}
		if serr := c.h.sleep(ctx, pollInterval); serr != nil {
			return "", fmt.Errorf("servicetest: %s manager did not finish booting, last state %q: %w", // [lydite:exclude_from_mutation][boot fails the test without reading the state when the error is set, so the state returned with it is unobservable]
				managerName(user), state, errors.Join(serr, err))
		}
	}
}

// managerName names the system or the user manager in messages.
func managerName(user bool) string {
	if user {
		return "user"
	}
	return "system"
}

// lookupUID sets c.uid to the uid of userName.
func (c *container) lookupUID(ctx context.Context) error {
	out, err := c.exec(ctx, execOptions{}, "id", "-u", userName)
	if err != nil {
		return fmt.Errorf("servicetest: look up the uid of %s: %w", userName, err)
	}
	uid := strings.TrimSpace(string(out))
	if uid == "" || strings.Trim(uid, "0123456789") != "" {
		return fmt.Errorf("servicetest: look up the uid of %s: id printed %q", userName, out)
	}
	c.uid = uid
	return nil
}

// userManagerUnit is the unit of userName's user manager.
func (c *container) userManagerUnit() string {
	return "user@" + c.uid + ".service"
}

// waitUserManager waits until userName's user manager unit is active; its
// linger file makes systemd-logind start it at boot.
func (c *container) waitUserManager(ctx context.Context) error {
	unit := c.userManagerUnit()
	for {
		out, err := c.exec(ctx, execOptions{}, "systemctl", "is-active", unit)
		state := strings.TrimSpace(string(out))
		if state == "active" {
			return nil
		}
		if serr := c.h.sleep(ctx, pollInterval); serr != nil {
			return fmt.Errorf("servicetest: %s did not become active, last state %q: %w",
				unit, state, errors.Join(serr, err))
		}
	}
}

// diagnostics returns the status and the journal of unit, a system unit or,
// when user is true, a unit of userName's user manager. A command that fails
// contributes its error; the text is for a test log, never parsed.
func (c *container) diagnostics(ctx context.Context, unit string, user bool) string {
	journal := []string{"journalctl", "--no-pager", "--lines", diagnosticLines}
	if user {
		journal = append(journal, "--user-unit", unit)
	} else {
		journal = append(journal, "--unit", unit)
	}
	return c.collect(ctx,
		command{opts: execOptions{user: user}, argv: systemctl(user, "status", "--no-pager", "--full", unit)},
		command{argv: journal},
	)
}

// bootDiagnostics returns what shows why the container did not boot: the
// console output systemd wrote, the failed units and the journal.
func (c *container) bootDiagnostics(ctx context.Context) string {
	var b strings.Builder
	out, err := c.h.r.run(ctx, nil, "logs", "--tail", diagnosticLines, c.id)
	writeSection(&b, "podman logs", out, err)
	b.WriteString(c.collect(ctx,
		command{argv: systemctl(false, "--failed", "--no-pager")},
		command{argv: []string{"journalctl", "--no-pager", "--boot", "--lines", diagnosticLines}},
	))
	return b.String()
}

// command is one command a diagnostic runs in the container.
type command struct {
	opts execOptions
	argv []string
}

// collect runs every command and returns their outputs, each under a
// heading naming the command.
func (c *container) collect(ctx context.Context, cmds ...command) string {
	var b strings.Builder
	for _, cmd := range cmds {
		out, err := c.exec(ctx, cmd.opts, cmd.argv...)
		writeSection(&b, strings.Join(cmd.argv, " "), out, err)
	}
	return b.String()
}

// writeSection writes out under the heading title, followed by err when it
// is non-nil.
func writeSection(b *strings.Builder, title string, out []byte, err error) {
	fmt.Fprintf(b, "--- %s\n%s", title, out)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		b.WriteByte('\n')
	}
	if err != nil {
		fmt.Fprintf(b, "(%v)\n", err)
	}
}

// logDiagnostics logs the diagnostics of unit to tb.
func (c *container) logDiagnostics(ctx context.Context, tb testing.TB, unit string, user bool) {
	tb.Helper()
	tb.Logf("diagnostics of %s in container %s:\n%s", unit, c.id, c.diagnostics(ctx, unit, user))
}

// boot starts a container of image for scope and returns it once its system
// manager, and for ScopeUser userName's user manager, has booted. It
// registers the container's removal with tb.Cleanup before it waits, and
// fails tb, with the boot diagnostics, when the container does not boot
// within timeout.
func boot(tb testing.TB, h harness, image string, scope service.Scope, timeout time.Duration) *container {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), timeout)
	defer cancel()
	c, err := h.start(ctx, image, scope)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		// tb's context is cancelled before cleanups run.
		rctx, rcancel := context.WithTimeout(context.Background(), removeTimeout)
		defer rcancel()
		if err := c.remove(rctx); err != nil {
			tb.Error(err)
		}
	})
	fail := func(err error) {
		tb.Helper()
		dctx, dcancel := context.WithTimeout(context.Background(), removeTimeout)
		defer dcancel()
		tb.Logf("boot diagnostics of container %s:\n%s", c.id, c.bootDiagnostics(dctx))
		tb.Fatal(err)
	}
	state, err := c.waitRunning(ctx, false)
	if err != nil {
		fail(err)
	}
	if state == "degraded" {
		out, _ := c.exec(ctx, execOptions{}, systemctl(false, "--failed", "--no-pager")...)
		tb.Logf("system manager of container %s is degraded:\n%s", c.id, out)
	}
	if scope != service.ScopeUser {
		return c
	}
	if err := c.lookupUID(ctx); err != nil {
		fail(err)
	}
	if err := c.waitUserManager(ctx); err != nil {
		fail(err)
	}
	state, err = c.waitRunning(ctx, true)
	if err != nil {
		fail(err)
	}
	if state == "degraded" {
		out, _ := c.exec(ctx, execOptions{user: true}, systemctl(true, "--failed", "--no-pager")...)
		tb.Logf("user manager of container %s is degraded:\n%s", c.id, out)
	}
	return c
}

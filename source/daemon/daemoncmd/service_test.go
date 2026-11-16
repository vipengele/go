package daemoncmd_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/speccheck"
	"go.vipengele.org/daemon/daemoncmd"
	"go.vipengele.org/daemon/service"
)

// fakeManager is a service.Manager that records every call, the ManagerFunc's and the hooks'
// included, and fails a method when errs holds an error under its name.
type fakeManager struct {
	calls  []string
	errs   map[string]error
	status service.Status
}

func (f *fakeManager) call(name string) error {
	f.calls = append(f.calls, name)
	return f.errs[name]
}

func (f *fakeManager) Prepare(context.Context) error         { return f.call("Prepare") }
func (f *fakeManager) Install(context.Context) error         { return f.call("Install") }
func (f *fakeManager) Uninstall(context.Context) error       { return f.call("Uninstall") }
func (f *fakeManager) Start(context.Context) error           { return f.call("Start") }
func (f *fakeManager) Stop(context.Context) error            { return f.call("Stop") }
func (f *fakeManager) Restart(context.Context) error         { return f.call("Restart") }
func (f *fakeManager) CheckPrivileges(context.Context) error { return f.call("CheckPrivileges") }

func (f *fakeManager) Status(context.Context) (service.Status, error) {
	return f.status, f.call("Status")
}

// managerFunc returns a ManagerFunc that records its call and returns f.
func (f *fakeManager) managerFunc() daemoncmd.ManagerFunc {
	return func(context.Context, *spec.Invocation) (service.Manager, error) {
		return f, f.call("manager")
	}
}

// hook returns a Hook that records name and returns err.
func (f *fakeManager) hook(name string, err error) daemoncmd.Hook {
	return func(_ context.Context, _ *spec.Invocation, m service.Manager) error {
		if m != service.Manager(f) {
			return fmt.Errorf("hook %s got manager %v, want the fake", name, m)
		}
		f.calls = append(f.calls, name)
		return err
	}
}

// failing returns a ManagerFunc that returns err.
func failing(err error) daemoncmd.ManagerFunc {
	return func(context.Context, *spec.Invocation) (service.Manager, error) { return nil, err }
}

// runOutput runs cmd with opts and flag values and returns what it wrote to stdout and stderr.
func runOutput(t *testing.T, cmd *spec.Command, opts spec.OutputOptions, values map[string]any) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdout: &out, Stderr: &errOut}, nil, values)
	opts.NoColorStdout, opts.NoColorStderr = true, true
	inv.Output = spec.NewOutput(opts)
	err = cmd.Run(t.Context(), inv)
	return out.String(), errOut.String(), err
}

// serviceCommand is a constructor of one service command.
type serviceCommand struct {
	name string
	new  func(daemoncmd.ManagerFunc, ...daemoncmd.ServiceOption) *spec.Command
	// mutating is the Manager method the command calls after CheckPrivileges, or "" for status.
	mutating string
}

func serviceCommands() []serviceCommand {
	return []serviceCommand{
		{name: "install", new: daemoncmd.NewInstall, mutating: "Install"},
		{name: "uninstall", new: daemoncmd.NewUninstall, mutating: "Uninstall"},
		{name: "start", new: daemoncmd.NewStart, mutating: "Start"},
		{name: "stop", new: daemoncmd.NewStop, mutating: "Stop"},
		{name: "restart", new: daemoncmd.NewRestart, mutating: "Restart"},
		{name: "status", new: daemoncmd.NewServiceStatus},
	}
}

// actions are the commands that call one Manager method after CheckPrivileges and print nothing.
func actions() []serviceCommand {
	return slices.DeleteFunc(serviceCommands(), func(c serviceCommand) bool {
		return c.name == "install" || c.name == "status"
	})
}

func runningStatus() service.Status {
	return service.Status{State: service.StateRunning, Enabled: true, PID: 4242, Restarts: 1, Detail: "active (running)"}
}

const runningText = "" +
	"FIELD     VALUE\n" +
	"state     running\n" +
	"enabled   true\n" +
	"pid       4242\n" +
	"restarts  1\n" +
	"detail    active (running)\n"

func TestServiceCommandsAreValidWithTheirNames(t *testing.T) {
	f := &fakeManager{}
	for _, c := range serviceCommands() {
		cmd := c.new(f.managerFunc())
		if err := cmd.Validate(); err != nil {
			t.Errorf("%s: Validate: %v", c.name, err)
		}
		if cmd.Name != c.name {
			t.Errorf("Name = %q, want %q", cmd.Name, c.name)
		}
		if len(cmd.Flags) != 0 {
			t.Errorf("%s: Flags = %v, want none", c.name, cmd.Flags)
		}
	}
}

func TestNewServiceGroupsTheSixCommands(t *testing.T) {
	f := &fakeManager{}
	cmd := daemoncmd.NewService(f.managerFunc())
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cmd.Name != "service" {
		t.Errorf("Name = %q, want service", cmd.Name)
	}
	var names []string
	for _, sub := range cmd.Commands {
		names = append(names, sub.Name)
	}
	want := []string{"install", "uninstall", "start", "stop", "restart", "status"}
	if !slices.Equal(names, want) {
		t.Errorf("subcommands = %v, want %v", names, want)
	}
}

func TestNewServiceSubcommandsActOnTheManager(t *testing.T) {
	f := &fakeManager{status: runningStatus()}
	cmd := daemoncmd.NewService(f.managerFunc())
	for i, c := range serviceCommands() {
		f.calls = nil
		if _, _, err := run(t, cmd.Commands[i], false); err != nil {
			t.Errorf("%s: Run = %v", c.name, err)
		}
		if c.mutating != "" && !slices.Contains(f.calls, c.mutating) {
			t.Errorf("%s: calls = %v, want %s", c.name, f.calls, c.mutating)
		}
	}
}

func TestNewServiceForwardsOptionsToInstall(t *testing.T) {
	f := &fakeManager{status: runningStatus()}
	listen := spec.StringFlag("listen", "address to listen on")
	cmd := daemoncmd.NewService(f.managerFunc(),
		daemoncmd.WithInstallFlags(listen),
		daemoncmd.WithBeforeInstall(f.hook("before", nil)),
		daemoncmd.WithAfterInstall(f.hook("after", nil)))
	install := cmd.Commands[0]
	if len(install.Flags) != 1 || install.Flags[0] != spec.Flag(listen) {
		t.Errorf("install Flags = %v, want --listen", install.Flags)
	}
	if _, _, err := run(t, install, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Contains(f.calls, "before") || !slices.Contains(f.calls, "after") {
		t.Errorf("calls = %v, want both hooks", f.calls)
	}
}

func TestConstructorsPanicOnNilManagerFuncNamingThemselves(t *testing.T) {
	for name, newCmd := range map[string]func(daemoncmd.ManagerFunc, ...daemoncmd.ServiceOption) *spec.Command{
		"NewService":       daemoncmd.NewService,
		"NewInstall":       daemoncmd.NewInstall,
		"NewUninstall":     daemoncmd.NewUninstall,
		"NewStart":         daemoncmd.NewStart,
		"NewStop":          daemoncmd.NewStop,
		"NewRestart":       daemoncmd.NewRestart,
		"NewServiceStatus": daemoncmd.NewServiceStatus,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				want := "daemoncmd: " + name + ": m is nil"
				if got := recover(); got != want {
					t.Fatalf("%s(nil) panicked with %v, want %q", name, got, want)
				}
			}()
			newCmd(nil)
		})
	}
}

func TestServiceStatusRenamedValidatesBesideHealthStatus(t *testing.T) {
	f := &fakeManager{}
	both := func(rename bool) *spec.Command {
		svc := daemoncmd.NewServiceStatus(f.managerFunc())
		if rename {
			svc.Name = "service-status"
		}
		return &spec.Command{
			Name:     "tool",
			Commands: []*spec.Command{daemoncmd.NewStatus(at("http://127.0.0.1:1")), svc},
		}
	}
	if err := both(true).Validate(); err != nil {
		t.Errorf("renamed: Validate = %v, want nil", err)
	}
	if err := both(false).Validate(); err == nil {
		t.Error("not renamed: Validate = nil, want the duplicate name reported")
	}
}

func TestInstallRunsEveryStepInOrder(t *testing.T) {
	f := &fakeManager{status: runningStatus()}
	cmd := daemoncmd.NewInstall(f.managerFunc(),
		daemoncmd.WithBeforeInstall(f.hook("before-1", nil)),
		daemoncmd.WithAfterInstall(f.hook("after-1", nil)),
		daemoncmd.WithBeforeInstall(f.hook("before-2", nil)),
		daemoncmd.WithAfterInstall(f.hook("after-2", nil)))
	stdout, stderr, err := run(t, cmd, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{
		"manager", "CheckPrivileges", "Prepare", "before-1", "before-2",
		"Install", "after-1", "after-2", "Status",
	}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %v\nwant    %v", f.calls, want)
	}
	if stdout != runningText {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, runningText)
	}
	if stderr != "✓ service installed and started\n" {
		t.Errorf("stderr = %q, want the success message", stderr)
	}
}

func TestInstallAbortsAtTheFirstFailure(t *testing.T) {
	errStep := errors.New("step failed")
	tests := []struct {
		name      string
		errs      map[string]error
		before    error
		after     error
		wantCalls []string
		// wantMsg is a substring the error carries.
		wantMsg string
	}{
		{
			name:      "privileges",
			errs:      map[string]error{"CheckPrivileges": errStep},
			wantCalls: []string{"manager", "CheckPrivileges"},
		},
		{
			name:      "prepare",
			errs:      map[string]error{"Prepare": errStep},
			wantCalls: []string{"manager", "CheckPrivileges", "Prepare"},
		},
		{
			name:      "before hook",
			before:    errStep,
			wantCalls: []string{"manager", "CheckPrivileges", "Prepare", "before-1"},
			wantMsg:   "the service was not installed",
		},
		{
			name:      "install",
			errs:      map[string]error{"Install": errStep},
			wantCalls: []string{"manager", "CheckPrivileges", "Prepare", "before-1", "before-2", "Install"},
		},
		{
			name:    "after hook",
			after:   errStep,
			wantMsg: "the service is installed, but a step after installing it failed",
			wantCalls: []string{
				"manager", "CheckPrivileges", "Prepare", "before-1", "before-2", "Install", "after-1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeManager{errs: tt.errs, status: runningStatus()}
			cmd := daemoncmd.NewInstall(f.managerFunc(),
				daemoncmd.WithBeforeInstall(f.hook("before-1", tt.before)),
				daemoncmd.WithBeforeInstall(f.hook("before-2", nil)),
				daemoncmd.WithAfterInstall(f.hook("after-1", tt.after)),
				daemoncmd.WithAfterInstall(f.hook("after-2", nil)))
			stdout, stderr, err := run(t, cmd, false)
			if !errors.Is(err, errStep) {
				t.Fatalf("err = %v, want the step's error", err)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err = %q, want it to say %q", err, tt.wantMsg)
			}
			if !strings.Contains(err.Error(), errStep.Error()) {
				t.Errorf("err = %q, want it to carry %q", err, errStep)
			}
			if !slices.Equal(f.calls, tt.wantCalls) {
				t.Errorf("calls = %v\nwant    %v", f.calls, tt.wantCalls)
			}
			if stdout != "" || stderr != "" {
				t.Errorf("stdout = %q, stderr = %q, want both empty", stdout, stderr)
			}
		})
	}
}

func TestInstallWithAnUnreadableStatusWarnsAndSucceeds(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		f := &fakeManager{errs: map[string]error{"Status": service.ErrManagerUnreachable}}
		stdout, stderr, err := run(t, daemoncmd.NewInstall(f.managerFunc()), asJSON)
		if err != nil {
			t.Fatalf("json=%v: Run = %v, want nil", asJSON, err)
		}
		if stdout != "" {
			t.Errorf("json=%v: stdout = %q, want empty", asJSON, stdout)
		}
		want := "⚠ the service is installed, but its status could not be read: " +
			service.ErrManagerUnreachable.Error() + "\n✓ service installed and started\n"
		if stderr != want {
			t.Errorf("json=%v: stderr = %q, want %q", asJSON, stderr, want)
		}
	}
}

func TestInstallJSONPrintsTheStatus(t *testing.T) {
	f := &fakeManager{status: runningStatus()}
	stdout, stderr, err := run(t, daemoncmd.NewInstall(f.managerFunc()), true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := `{
  "schema": 1,
  "state": "running",
  "enabled": true,
  "pid": 4242,
  "restarts": 1,
  "detail": "active (running)"
}
`
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if stderr != "✓ service installed and started\n" {
		t.Errorf("stderr = %q, want the success message", stderr)
	}
}

func TestInstallFlagsReachTheManagerFunc(t *testing.T) {
	listen := spec.StringFlag("listen", "address to listen on")
	f := &fakeManager{status: runningStatus()}
	var got string
	m := func(ctx context.Context, inv *spec.Invocation) (service.Manager, error) {
		got = listen.Get(inv)
		return f, nil
	}
	cmd := daemoncmd.NewInstall(m, daemoncmd.WithInstallFlags(listen))
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, _, err := runOutput(t, cmd, spec.OutputOptions{}, map[string]any{"listen": ":9000"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != ":9000" {
		t.Errorf("ManagerFunc read --listen = %q, want :9000", got)
	}
}

func TestInstallSkipsNilOptionsHooksAndFlags(t *testing.T) {
	listen := spec.StringFlag("listen", "address to listen on")
	port := spec.IntFlag("port", "port to listen on")
	f := &fakeManager{status: runningStatus()}
	cmd := daemoncmd.NewInstall(f.managerFunc(),
		nil,
		daemoncmd.WithBeforeInstall(nil),
		daemoncmd.WithAfterInstall(nil),
		daemoncmd.WithInstallFlags(listen, nil),
		daemoncmd.WithInstallFlags(port))
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if want := []spec.Flag{listen, port}; !slices.Equal(cmd.Flags, want) {
		t.Errorf("Flags = %v, want %v", cmd.Flags, want)
	}
	if _, _, err := run(t, cmd, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"manager", "CheckPrivileges", "Prepare", "Install", "Status"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestInstallOnlyOptionsAreIgnoredElsewhere(t *testing.T) {
	for _, c := range serviceCommands()[1:] {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeManager{status: runningStatus()}
			cmd := c.new(f.managerFunc(),
				daemoncmd.WithBeforeInstall(f.hook("before", nil)),
				daemoncmd.WithAfterInstall(f.hook("after", nil)),
				daemoncmd.WithInstallFlags(spec.StringFlag("listen", "address to listen on")))
			if len(cmd.Flags) != 0 {
				t.Errorf("Flags = %v, want none", cmd.Flags)
			}
			if _, _, err := run(t, cmd, false); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if slices.Contains(f.calls, "before") || slices.Contains(f.calls, "after") {
				t.Errorf("calls = %v, want no hook", f.calls)
			}
		})
	}
}

func TestEveryServiceCommandRefusesInAContainer(t *testing.T) {
	instead := "run it as the image's entrypoint"
	for _, c := range serviceCommands() {
		for _, asJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%v", c.name, asJSON), func(t *testing.T) {
				cmd := c.new(failing(&service.ContainerError{Instead: instead}))
				stdout, stderr, err := run(t, cmd, asJSON)
				assertOrdinary(t, err, stdout)
				if want := "service management is unavailable in a container: " + instead; err.Error() != want {
					t.Errorf("err = %q, want %q", err, want)
				}
				if !errors.Is(err, service.ErrContainer) {
					t.Errorf("err = %v, want it to match ErrContainer", err)
				}
				if stderr != "" {
					t.Errorf("stderr = %q, want empty: the App reports the error", stderr)
				}
			})
		}
	}
}

func TestContainerRefusalWithoutAContainerErrorNamesNoAdvice(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("build manager: %w", service.ErrContainer),
		&service.ContainerError{},
	} {
		_, _, got := run(t, daemoncmd.NewServiceStatus(failing(err)), false)
		if want := "service management is unavailable in a container"; got == nil || got.Error() != want {
			t.Errorf("%v: err = %v, want %q", err, got, want)
		}
		if !errors.Is(got, service.ErrContainer) {
			t.Errorf("%v: err = %v, want it to match ErrContainer", err, got)
		}
	}
}

func TestEveryServiceCommandReturnsAManagerFuncErrorUnchanged(t *testing.T) {
	for _, want := range []error{service.ErrUnsupported, service.ErrInvalidConfig} {
		for _, c := range serviceCommands() {
			stdout, _, err := run(t, c.new(failing(want)), false)
			assertOrdinary(t, err, stdout)
			if err != want {
				t.Errorf("%s: err = %v, want %v unchanged", c.name, err, want)
			}
		}
	}
}

func TestPrivilegesAreCheckedBeforeAnyChange(t *testing.T) {
	for _, c := range serviceCommands() {
		if c.mutating == "" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			f := &fakeManager{errs: map[string]error{
				"CheckPrivileges": service.ErrPrivilegesRequired,
				c.mutating:        service.ErrNotInstalled,
			}}
			cmd := c.new(f.managerFunc(), daemoncmd.WithBeforeInstall(f.hook("before", nil)))
			stdout, stderr, err := run(t, cmd, false)
			assertOrdinary(t, err, stdout)
			if err != service.ErrPrivilegesRequired {
				t.Errorf("err = %v, want ErrPrivilegesRequired unchanged", err)
			}
			if want := []string{"manager", "CheckPrivileges"}; !slices.Equal(f.calls, want) {
				t.Errorf("calls = %v, want %v", f.calls, want)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestActionsCallOneMethodAndReportSuccess(t *testing.T) {
	success := map[string]string{
		"uninstall": "✓ service uninstalled; its data, account and logs are left in place\n",
		"start":     "✓ service started\n",
		"stop":      "✓ service stopped\n",
		"restart":   "✓ service restarted\n",
	}
	for _, c := range actions() {
		for _, asJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%v", c.name, asJSON), func(t *testing.T) {
				f := &fakeManager{}
				stdout, stderr, err := run(t, c.new(f.managerFunc()), asJSON)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if want := []string{"manager", "CheckPrivileges", c.mutating}; !slices.Equal(f.calls, want) {
					t.Errorf("calls = %v, want %v", f.calls, want)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want empty", stdout)
				}
				if stderr != success[c.name] {
					t.Errorf("stderr = %q, want %q", stderr, success[c.name])
				}
			})
		}
	}
}

func TestActionErrorsAreReturnedUnchanged(t *testing.T) {
	for _, c := range actions() {
		f := &fakeManager{errs: map[string]error{c.mutating: service.ErrManagerUnreachable}}
		stdout, stderr, err := run(t, c.new(f.managerFunc()), false)
		assertOrdinary(t, err, stdout)
		if err != service.ErrManagerUnreachable {
			t.Errorf("%s: err = %v, want ErrManagerUnreachable unchanged", c.name, err)
		}
		if stderr != "" {
			t.Errorf("%s: stderr = %q, want empty", c.name, stderr)
		}
	}
}

func TestStartStopRestartOfANotInstalledServiceSaysToInstall(t *testing.T) {
	notInstalled := fmt.Errorf("no unit file: %w", service.ErrNotInstalled)
	for _, c := range actions() {
		if c.name == "uninstall" {
			continue
		}
		f := &fakeManager{errs: map[string]error{c.mutating: notInstalled}}
		stdout, stderr, err := run(t, c.new(f.managerFunc()), false)
		assertOrdinary(t, err, stdout)
		if want := "the service is not installed; install it first"; err.Error() != want {
			t.Errorf("%s: err = %q, want %q", c.name, err, want)
		}
		if !errors.Is(err, notInstalled) {
			t.Errorf("%s: err = %v, want it to wrap the manager's error", c.name, err)
		}
		if stderr != "" {
			t.Errorf("%s: stderr = %q, want empty", c.name, stderr)
		}
	}
}

func TestUninstallOfANotInstalledServiceSucceeds(t *testing.T) {
	f := &fakeManager{errs: map[string]error{"Uninstall": fmt.Errorf("no unit file: %w", service.ErrNotInstalled)}}
	stdout, stderr, err := run(t, daemoncmd.NewUninstall(f.managerFunc()), false)
	if err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if want := "ℹ not installed; nothing to remove\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestUninstallCallsNoOtherMutatingMethod(t *testing.T) {
	for _, errUninstall := range []error{nil, service.ErrNotInstalled, service.ErrManagerUnreachable} {
		f := &fakeManager{errs: map[string]error{"Uninstall": errUninstall}}
		_, _, _ = run(t, daemoncmd.NewUninstall(f.managerFunc()), false)
		if want := []string{"manager", "CheckPrivileges", "Uninstall"}; !slices.Equal(f.calls, want) {
			t.Errorf("Uninstall error %v: calls = %v, want %v", errUninstall, f.calls, want)
		}
	}
}

func TestQuietDropsEveryMessageButKeepsTheStatus(t *testing.T) {
	quiet := spec.OutputOptions{Quiet: true}
	for _, c := range serviceCommands() {
		f := &fakeManager{status: runningStatus()}
		stdout, stderr, err := runOutput(t, c.new(f.managerFunc()), quiet, nil)
		if err != nil {
			t.Fatalf("%s: Run: %v", c.name, err)
		}
		if stderr != "" {
			t.Errorf("%s: stderr = %q, want empty", c.name, stderr)
		}
		wantStdout := ""
		if c.name == "install" || c.name == "status" {
			wantStdout = runningText
		}
		if stdout != wantStdout {
			t.Errorf("%s: stdout = %q, want %q", c.name, stdout, wantStdout)
		}
	}

	f := &fakeManager{errs: map[string]error{"Uninstall": service.ErrNotInstalled}}
	if _, stderr, err := runOutput(t, daemoncmd.NewUninstall(f.managerFunc()), quiet, nil); err != nil || stderr != "" {
		t.Errorf("uninstall of nothing: err = %v, stderr = %q, want nil and empty", err, stderr)
	}
	f = &fakeManager{errs: map[string]error{"Status": errors.New("unreadable")}}
	if _, stderr, err := runOutput(t, daemoncmd.NewInstall(f.managerFunc()), quiet, nil); err != nil || stderr != "" {
		t.Errorf("install with unreadable status: err = %v, stderr = %q, want nil and empty", err, stderr)
	}
}

func TestServiceStatusSucceedsForEveryState(t *testing.T) {
	for _, state := range []service.State{
		service.StateUnknown, service.StateNotInstalled, service.StateStopped,
		service.StateStarting, service.StateRunning, service.StateFailed,
	} {
		for _, asJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%v", state, asJSON), func(t *testing.T) {
				f := &fakeManager{status: service.Status{State: state}}
				stdout, stderr, err := run(t, daemoncmd.NewServiceStatus(f.managerFunc()), asJSON)
				if err != nil {
					t.Fatalf("Run = %v, want nil", err)
				}
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
				if want := []string{"manager", "Status"}; !slices.Equal(f.calls, want) {
					t.Errorf("calls = %v, want %v: status checks no privileges", f.calls, want)
				}
				if !asJSON {
					if !strings.Contains(stdout, "state     "+state.String()+"\n") {
						t.Errorf("stdout does not show %s:\n%s", state, stdout)
					}
					return
				}
				var got struct {
					Schema int `json:"schema"`
					service.Status
				}
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
				}
				if got.Schema != 1 || got.Status != f.status {
					t.Errorf("JSON = %+v, want schema 1 and %+v", got, f.status)
				}
			})
		}
	}
}

func TestServiceStatusText(t *testing.T) {
	f := &fakeManager{status: runningStatus()}
	stdout, _, err := run(t, daemoncmd.NewServiceStatus(f.managerFunc()), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stdout != runningText {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, runningText)
	}
}

func TestServiceStatusJSONKeys(t *testing.T) {
	f := &fakeManager{status: runningStatus()}
	stdout, _, _ := run(t, daemoncmd.NewServiceStatus(f.managerFunc()), true)
	speccheck.AssertJSONKeys(t, json.RawMessage(stdout), "testdata/service-status.keys")
}

func TestServiceStatusQueryErrorIsReturned(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		f := &fakeManager{
			status: service.Status{State: service.StateUnknown, Detail: "no session bus"},
			errs:   map[string]error{"Status": service.ErrManagerUnreachable},
		}
		stdout, _, err := run(t, daemoncmd.NewServiceStatus(f.managerFunc()), asJSON)
		assertOrdinary(t, err, stdout)
		if err != service.ErrManagerUnreachable {
			t.Errorf("json=%v: err = %v, want ErrManagerUnreachable unchanged", asJSON, err)
		}
	}
}

func TestServiceStatusWriteErrorIsReturned(t *testing.T) {
	for _, c := range []serviceCommand{serviceCommands()[0], serviceCommands()[5]} {
		for _, asJSON := range []bool{false, true} {
			f := &fakeManager{status: runningStatus()}
			errWrite := errors.New("broken pipe")
			err := runTo(t, c.new(f.managerFunc()), asJSON, &limitWriter{err: errWrite})
			if !errors.Is(err, errWrite) {
				t.Errorf("%s json=%v: err = %v, want it to wrap the write error", c.name, asJSON, err)
			}
		}
	}
}

func TestManagerFuncIsCalledOncePerRun(t *testing.T) {
	for _, c := range serviceCommands() {
		f := &fakeManager{status: runningStatus()}
		cmd := c.new(f.managerFunc())
		for range 2 {
			if _, _, err := run(t, cmd, false); err != nil {
				t.Fatalf("%s: Run: %v", c.name, err)
			}
		}
		var n int
		for _, call := range f.calls {
			if call == "manager" {
				n++
			}
		}
		if n != 2 {
			t.Errorf("%s: ManagerFunc called %d times in two runs, want 2", c.name, n)
		}
	}
}

// ExampleNewService mounts the service commands two ways: grouped under "service", and flat beside
// the health "status", with the service's own status renamed. The install command takes a --listen
// flag that reaches the service's arguments through the ManagerFunc, and a before-install hook
// writes the daemon's API token to a file only the service's account may read, before Install
// starts the daemon. The token never goes into Config.Env, which every local account can read.
func ExampleNewService() {
	const (
		name      = "exampled"
		binary    = "/usr/local/bin/exampled"
		tokenFile = "/etc/exampled/token"
	)
	listen := spec.StringFlag("listen", "address the daemon listens on", spec.Default("127.0.0.1:8080"))

	manager := func(_ context.Context, inv *spec.Invocation) (service.Manager, error) {
		return service.New(service.Config{
			Name:   name,
			Binary: binary,
			Args:   []string{"serve", "--listen", listen.Get(inv), "--token-file", tokenFile},
			// Env is readable by every local account: it carries no secret.
			Env: map[string]string{"EXAMPLED_LOG_FORMAT": "json"},
		})
	}
	writeToken := func(context.Context, *spec.Invocation, service.Manager) error {
		if _, err := os.Stat(tokenFile); err == nil {
			// A re-run of install updates the service and keeps the token its clients hold.
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
			return err
		}
		return os.WriteFile(tokenFile, []byte(rand.Text()+"\n"), 0o600)
	}
	opts := []daemoncmd.ServiceOption{
		daemoncmd.WithInstallFlags(listen),
		daemoncmd.WithBeforeInstall(writeToken),
	}

	grouped := &spec.Command{Name: name, Commands: []*spec.Command{daemoncmd.NewService(manager, opts...)}}

	serviceStatus := daemoncmd.NewServiceStatus(manager)
	serviceStatus.Name = "service-status"
	flat := &spec.Command{Name: name, Commands: []*spec.Command{
		daemoncmd.NewStatus(func(context.Context, *spec.Invocation) (string, error) {
			return "http://127.0.0.1:8080", nil
		}),
		daemoncmd.NewInstall(manager, opts...),
		daemoncmd.NewUninstall(manager),
		daemoncmd.NewStart(manager),
		daemoncmd.NewStop(manager),
		daemoncmd.NewRestart(manager),
		serviceStatus,
	}}

	for _, root := range []*spec.Command{grouped, flat} {
		if err := root.Validate(); err != nil {
			fmt.Println(err)
			return
		}
	}
	names := func(cmds []*spec.Command) string {
		var out []string
		for _, c := range cmds {
			out = append(out, c.Name)
		}
		return strings.Join(out, " ")
	}
	fmt.Println("grouped:", names(grouped.Commands[0].Commands))
	fmt.Println("flat:   ", names(flat.Commands))
	// Output:
	// grouped: install uninstall start stop restart status
	// flat:    status install uninstall start stop restart service-status
}

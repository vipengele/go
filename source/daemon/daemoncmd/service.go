package daemoncmd

import (
	"context"
	"errors"
	"io"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/table"
	"go.vipengele.org/daemon/service"
)

// ManagerFunc returns the [service.Manager] one run of a service command acts on. It is called
// once per run, before the command does anything else, so the tool builds the manager there with
// [service.New] from the invocation: the flags [WithInstallFlags] declares on install reach the
// service's [service.Config] only through it. An error matching [service.ErrContainer] becomes
// the command's container refusal; any other error is returned unchanged.
type ManagerFunc func(ctx context.Context, inv *spec.Invocation) (service.Manager, error)

// Hook is a step of install that runs around [service.Manager.Install], given the run's
// invocation and the manager it acts on. An error aborts the install.
type Hook func(ctx context.Context, inv *spec.Invocation, m service.Manager) error

// ServiceOption configures the commands [NewService] and its flat counterparts return. Every
// option is install-only: [NewInstall] and the install command of [NewService] apply them, and
// [NewUninstall], [NewStart], [NewStop], [NewRestart] and [NewServiceStatus] accept and ignore
// them, so one option list can be passed to every constructor. A nil option is skipped.
type ServiceOption func(*serviceSettings)

// serviceSettings holds what the [ServiceOption]s given to a service command set.
type serviceSettings struct {
	before []Hook
	after  []Hook
	flags  []spec.Flag
}

// WithBeforeInstall runs hook during install after [service.Manager.Prepare] has created the
// service's directories and before [service.Manager.Install] registers and starts the service.
// Install starts the service, so whatever the daemon needs when it starts, such as a secret it
// reads from a file, is written by a before-install hook. A failing hook aborts the install with
// nothing registered. The option is repeatable: hooks run in the order they are given, and the
// first error stops the rest. A nil hook is skipped.
func WithBeforeInstall(hook Hook) ServiceOption {
	return func(s *serviceSettings) {
		if hook != nil {
			s.before = append(s.before, hook)
		}
	}
}

// WithAfterInstall runs hook during install after [service.Manager.Install] has returned, when the
// service is registered and running. A failing hook leaves the service installed, and the command
// returns its error. The option is repeatable: hooks run in the order they are given, and the
// first error stops the rest. A nil hook is skipped.
func WithAfterInstall(hook Hook) ServiceOption {
	return func(s *serviceSettings) {
		if hook != nil {
			s.after = append(s.after, hook)
		}
	}
}

// WithInstallFlags declares flags on the install command, which the [ManagerFunc] and the hooks
// read from the invocation. The option is repeatable and the flags keep their order. A nil flag is
// skipped.
func WithInstallFlags(flags ...spec.Flag) ServiceOption {
	return func(s *serviceSettings) {
		for _, f := range flags {
			if f != nil {
				s.flags = append(s.flags, f)
			}
		}
	}
}

// newServiceSettings applies opts in order.
func newServiceSettings(opts []ServiceOption) serviceSettings {
	var s serviceSettings
	for _, opt := range opts {
		if opt != nil {
			opt(&s)
		}
	}
	return s
}

// NewService returns the "service" command, which groups install, uninstall, start, stop,
// restart and status: the commands [NewInstall], [NewUninstall], [NewStart], [NewStop],
// [NewRestart] and [NewServiceStatus] return. opts reach install. It panics when m is nil.
func NewService(m ManagerFunc, opts ...ServiceOption) *spec.Command {
	mustManagerFunc("NewService", m)
	return &spec.Command{
		Name:    "service",
		Summary: "Manage the daemon as a service of the operating system",
		Description: "Install, uninstall, start, stop and restart the daemon as a service of the " +
			"operating system's service manager, and print its state there.",
		Commands: []*spec.Command{
			NewInstall(m, opts...),
			NewUninstall(m, opts...),
			NewStart(m, opts...),
			NewStop(m, opts...),
			NewRestart(m, opts...),
			NewServiceStatus(m, opts...),
		},
	}
}

// NewInstall returns the "install" command. It checks privileges, prepares the service's
// directories, runs the [WithBeforeInstall] hooks, installs the service, which starts it or, when
// it runs already, restarts it on the new configuration, runs the [WithAfterInstall] hooks, and
// prints the service's status as [NewServiceStatus] does, followed by a success message on the
// standard error. Running it again updates an installed service in place.
//
// Nothing is rolled back: an error from [service.Manager.Install] is returned as is, and an error
// from an after-install hook is returned with the service left installed. A status that cannot be
// read once the service is installed is a warning, and the command still succeeds. It panics
// when m is nil.
func NewInstall(m ManagerFunc, opts ...ServiceOption) *spec.Command {
	mustManagerFunc("NewInstall", m)
	s := newServiceSettings(opts)
	return &spec.Command{
		Name:    "install",
		Summary: "Install the daemon as a service and start it",
		Description: "Install the daemon as a service of the operating system and start it, or " +
			"restart it on the new configuration when it is installed already, then print its " +
			"state. Running it again updates the service in place.",
		Args:  spec.NoArgs(),
		Flags: s.flags,
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			mgr, err := privileged(ctx, m, inv)
			if err != nil {
				return err
			}
			if err := mgr.Prepare(ctx); err != nil {
				return err
			}
			if err := runHooks(ctx, inv, mgr, s.before); err != nil {
				return &messageError{msg: "the service was not installed: " + err.Error(), err: err}
			}
			if err := mgr.Install(ctx); err != nil {
				return err
			}
			if err := runHooks(ctx, inv, mgr, s.after); err != nil {
				return &messageError{
					msg: "the service is installed, but a step after installing it failed: " + err.Error(),
					err: err,
				}
			}
			st, err := mgr.Status(ctx)
			if err != nil {
				inv.Warning("the service is installed, but its status could not be read: %v", err)
			} else if err := writeServiceStatus(inv, st); err != nil {
				return err
			}
			inv.Success("service installed and started")
			return nil
		},
	}
}

// NewUninstall returns the "uninstall" command. It checks privileges and removes the service from
// the service manager with [service.Manager.Uninstall], which stops it first and leaves its data,
// its account and its logs in place; it prints nothing to the standard output and a success
// message on the standard error. A service that is not installed is not an error: the command
// says there is nothing to remove and succeeds. It panics when m is nil.
func NewUninstall(m ManagerFunc, _ ...ServiceOption) *spec.Command {
	mustManagerFunc("NewUninstall", m)
	return &spec.Command{
		Name:    "uninstall",
		Summary: "Stop the service and remove it from the service manager",
		Description: "Stop the service and remove it from the operating system's service manager. " +
			"Its data, its account and its logs stay in place.",
		Args: spec.NoArgs(),
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			mgr, err := privileged(ctx, m, inv)
			if err != nil {
				return err
			}
			if err := mgr.Uninstall(ctx); err != nil {
				if errors.Is(err, service.ErrNotInstalled) {
					inv.Info("not installed; nothing to remove")
					return nil
				}
				return err
			}
			inv.Success("service uninstalled; its data, account and logs are left in place")
			return nil
		},
	}
}

// NewStart returns the "start" command, which checks privileges and starts the installed service
// with [service.Manager.Start]. It prints nothing to the standard output and a success message on
// the standard error. A service that is not installed is an error wrapping
// [service.ErrNotInstalled]. It panics when m is nil.
func NewStart(m ManagerFunc, _ ...ServiceOption) *spec.Command {
	mustManagerFunc("NewStart", m)
	return action(m, "start", "Start the installed service",
		"Start the installed service.", service.Manager.Start, "service started")
}

// NewStop returns the "stop" command, which checks privileges and stops the installed service
// with [service.Manager.Stop]; the service stays installed and starts again at the next boot or
// login. It prints nothing to the standard output and a success message on the standard error. A
// service that is not installed is an error wrapping [service.ErrNotInstalled]. It panics when m
// is nil.
func NewStop(m ManagerFunc, _ ...ServiceOption) *spec.Command {
	mustManagerFunc("NewStop", m)
	return action(m, "stop", "Stop the installed service",
		"Stop the installed service. It stays installed, and starts again at the next boot or login.",
		service.Manager.Stop, "service stopped")
}

// NewRestart returns the "restart" command, which checks privileges and restarts the installed
// service with [service.Manager.Restart], starting it when it is stopped. It prints nothing to the
// standard output and a success message on the standard error. A service that is not installed is
// an error wrapping [service.ErrNotInstalled]. It panics when m is nil.
func NewRestart(m ManagerFunc, _ ...ServiceOption) *spec.Command {
	mustManagerFunc("NewRestart", m)
	return action(m, "restart", "Restart the installed service",
		"Restart the installed service, or start it when it is stopped.",
		service.Manager.Restart, "service restarted")
}

// NewServiceStatus returns a command named "status" that prints the state of the service as the
// operating system's service manager reports it, with [service.Manager.Status]; it needs no
// privileges. Its name is the one [NewStatus], which reports the daemon's health over HTTP, also
// takes, so a tool that mounts both under one parent sets the returned command's Name, to
// "service-status" for instance.
//
// The command prints a table of the state, whether the service is enabled, its process id, its
// restart count and the service manager's detail, or, when [spec.Output.Format] is
// [spec.FormatJSON], the [service.Status] with [spec.WriteJSON] at schema 1, under the keys
// state, enabled, pid, restarts and detail. It returns nil for every state, not_installed
// included, and an error only when the status cannot be read. It panics when m is nil.
func NewServiceStatus(m ManagerFunc, _ ...ServiceOption) *spec.Command {
	mustManagerFunc("NewServiceStatus", m)
	return &spec.Command{
		Name:    "status",
		Summary: "Print the state of the service",
		Description: "Print the state of the service as the operating system's service manager " +
			"reports it. Exits 0 whatever the state, and 1 only when it cannot be read.",
		Args: spec.NoArgs(),
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			mgr, err := manager(ctx, m, inv)
			if err != nil {
				return err
			}
			st, err := mgr.Status(ctx)
			if err != nil {
				return err
			}
			return writeServiceStatus(inv, st)
		},
	}
}

// action returns a command that checks privileges and calls act on the manager, turning a
// service that is not installed into an error that says to install it.
func action(m ManagerFunc, name, summary, description string,
	act func(service.Manager, context.Context) error, done string,
) *spec.Command {
	return &spec.Command{
		Name:        name,
		Summary:     summary,
		Description: description,
		Args:        spec.NoArgs(),
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			mgr, err := privileged(ctx, m, inv)
			if err != nil {
				return err
			}
			if err := act(mgr, ctx); err != nil {
				if errors.Is(err, service.ErrNotInstalled) {
					return &messageError{msg: "the service is not installed; install it first", err: err}
				}
				return err
			}
			inv.Success("%s", done)
			return nil
		},
	}
}

// mustManagerFunc panics when m is nil, naming the constructor.
func mustManagerFunc(constructor string, m ManagerFunc) {
	if m == nil {
		panic("daemoncmd: " + constructor + ": m is nil")
	}
}

// manager calls m, turning a refusal to manage a service in a container into an error that says
// what to do instead.
func manager(ctx context.Context, m ManagerFunc, inv *spec.Invocation) (service.Manager, error) {
	mgr, err := m(ctx, inv)
	if err == nil {
		return mgr, nil
	}
	if !errors.Is(err, service.ErrContainer) {
		return nil, err
	}
	msg := "service management is unavailable in a container"
	if ce, ok := errors.AsType[*service.ContainerError](err); ok && ce.Instead != "" {
		msg += ": " + ce.Instead
	}
	return nil, &messageError{msg: msg, err: err}
}

// privileged calls m and checks that the process has the privileges the manager's mutating
// methods need.
func privileged(ctx context.Context, m ManagerFunc, inv *spec.Invocation) (service.Manager, error) {
	mgr, err := manager(ctx, m, inv)
	if err != nil {
		return nil, err
	}
	if err := mgr.CheckPrivileges(ctx); err != nil {
		return nil, err
	}
	return mgr, nil
}

// runHooks runs hooks in order and returns the first error.
func runHooks(ctx context.Context, inv *spec.Invocation, mgr service.Manager, hooks []Hook) error {
	for _, hook := range hooks {
		if err := hook(ctx, inv, mgr); err != nil {
			return err
		}
	}
	return nil
}

// writeServiceStatus writes st to the standard output: a table of one row per field, or the
// status at schema 1 when the output is JSON.
func writeServiceStatus(inv *spec.Invocation, st service.Status) error {
	if inv.Output.Format() == spec.FormatJSON {
		return spec.WriteJSON(inv.Env.Stdout, schema, st)
	}
	return renderServiceStatus(inv.Env.Stdout, inv.Output.Styler(spec.Stdout), st)
}

// renderServiceStatus writes st as a two-column table of field and value.
func renderServiceStatus(w io.Writer, styler spec.Styler, st service.Status) error {
	t := table.New(
		table.Column{Key: "field", Header: "FIELD"},
		table.Column{Key: "value", Header: "VALUE"},
	)
	t.Add("state", st.State)
	t.Add("enabled", st.Enabled)
	t.Add("pid", st.PID)
	t.Add("restarts", st.Restarts)
	t.Add("detail", st.Detail)
	return t.Render(w, styler)
}

// messageError is an error whose message replaces the one of the error it wraps, which errors.Is
// and errors.As still see.
type messageError struct {
	msg string
	err error
}

func (e *messageError) Error() string { return e.msg }

func (e *messageError) Unwrap() error { return e.err }

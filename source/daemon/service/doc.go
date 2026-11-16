// Package service fronts the operating system's service manager through one
// driver per operating system behind the Manager interface: systemd on linux,
// launchd on darwin and the service control manager (SCM) on windows.
//
// A service is described by a Config, which New validates before it hands
// back a Manager; a Config that fails validation is reported as
// ErrInvalidConfig. New reports ErrUnsupported for a GOOS with no driver,
// which is every GOOS but linux, darwin and windows.
//
// # Containers
//
// Inside a container the container runtime supervises the process, and no
// service manager is there to install into. New refuses there before it looks
// at the Config or the GOOS: it returns a *ContainerError, which matches
// ErrContainer under errors.Is and whose Instead field says what to do in
// place of a service, which is to run the program as the image's entrypoint
// under a restart policy.
//
// New detects a container with go.vipengele.org/cli/spec/paths. WithIdentity
// names the tool, so that its user can override the detection with the
// <PREFIX>_CONTAINER environment variable, as paths.InContainerFor reads it;
// without it New uses the paths.InContainer heuristic alone. An identity that
// fails its Validate is reported as ErrInvalidConfig. WithContainer replaces
// detection altogether, and wins over the environment and WithIdentity; tests
// use it to keep New from probing the host.
//
// The state of an installed service is reported as a Status, whose State is
// one of a fixed set of values that marshal to lower-case text.
//
// # Scopes
//
// systemd: a system unit is installed in /etc/systemd/system, driven by
// systemctl, wanted by multi-user.target and ordered after
// network-online.target. Install, Uninstall, Start, Stop, Prepare and CheckPrivileges need
// root and report ErrPrivilegesRequired otherwise. A user unit is installed in
// $XDG_CONFIG_HOME/systemd/user (default $HOME/.config/systemd/user), driven
// by systemctl --user and wanted by default.target; it needs no root, and
// Config.User must be empty. Status reports ErrManagerUnreachable, with
// StateUnknown, when the user manager cannot be reached, as under sudo -u or
// cron.
//
// launchd: the job's label is Config.Name, and launchctl drives it. A
// ScopeSystem service is a LaunchDaemon in the system domain, whose property
// list is /Library/LaunchDaemons/<Name>.plist; it starts at boot and runs as
// Config.User, written as UserName, or as root when that is empty. Install,
// Uninstall, Start, Stop, Prepare and CheckPrivileges need root and
// report ErrPrivilegesRequired otherwise. A ScopeUser service is a LaunchAgent in the
// gui/<uid> domain of the user, whose property list is
// $HOME/Library/LaunchAgents/<Name>.plist; it needs no root, and Config.User
// must be empty. A user service is an agent because only an agent reaches the
// user's Keychain. launchd creates the gui domain only while the user is
// logged in to the GUI, which an ssh login or a sudo -u shell is not; Install,
// and Uninstall, Start, Stop and Status of an installed agent, then report
// ErrManagerUnreachable. A system-wide agent, one in
// /Library/LaunchAgents loaded for every user who logs in, is not supported.
//
// windows: a ScopeSystem service is an own-process service of the SCM named
// Config.Name, started automatically at boot. The driver reaches the SCM
// through golang.org/x/sys/windows/svc/mgr and the service's registry key, and
// compiles on every host; on a host other than Windows, New with
// WithGOOS("windows") returns it and every method fails with an error wrapping
// ErrUnsupported. ScopeUser and Config.WorkingDir are not supported: New
// returns an error wrapping ErrUnsupported that names the scope or the field.
// An empty Config.User runs the service as LocalSystem; otherwise it is
// NT AUTHORITY\LocalService, NT AUTHORITY\NetworkService or NT SERVICE\<Name>,
// compared without regard to case, and no password can be given. Any other
// User is reported as ErrInvalidConfig on every host. Install, Uninstall,
// Start, Stop, Prepare and CheckPrivileges need an elevated process and
// report ErrPrivilegesRequired otherwise; Status needs none.
//
// # Hardening
//
// The systemd driver writes sandboxing directives into the unit.
// DefaultHardening returns the defaults of a scope, and WithSystemdHardening
// replaces them; New validates the value on every GOOS, wrapping
// ErrInvalidConfig, and the other drivers ignore it. A system unit defaults to
// NoNewPrivileges, PrivateTmp, ProtectSystem=full, ProtectHome=read-only,
// ProtectKernelTunables, ProtectControlGroups and RestrictSUIDSGID. A user unit
// defaults to NoNewPrivileges only. An override may add more to a user unit,
// but the sandboxing directives need unprivileged user namespaces; where they
// are unavailable, systemd may refuse to start the unit or skip the directive,
// depending on its version.
//
// # Readiness
//
// WithSystemdNotify makes the unit Type=notify in place of Type=simple, so systemd waits for the
// service to report READY=1 (daemon.WithSystemd sends it). Given a watchdog interval it also
// writes WatchdogSec= in whole milliseconds, and the service must ping within it or systemd
// restarts it. New validates the option on every GOOS, wrapping ErrInvalidConfig, and the other
// drivers ignore it.
//
// # Environment
//
// The systemd and launchd drivers write Config.Env into a file every local
// account can read: the systemd unit, which "systemctl show" also reports, and
// the launchd property list, as EnvironmentVariables; both are mode 0644. The
// windows driver writes it to the Environment value of the service's registry
// key, which administrators can read. The values are therefore visible on the
// machine and must not carry secrets; a service that needs one reads it from a
// file only its account may read.
//
// # Logs
//
// systemd sends the service's output to the journal. launchd writes it to
// files: by default one file takes both standard output and standard error,
// $HOME/Library/Logs/<Name>/<Name>.log for an agent and
// /Library/Logs/<Name>/<Name>.log for a daemon. WithLaunchdLogs sets either
// stream's file, and an empty path keeps that stream's default. Prepare
// creates the log directories; Uninstall leaves the log files in place.
//
// # Lingering
//
// Lingering is a systemd setting. The systemd driver never turns it on. A
// user unit then stops when its user's last session ends, and Status says so
// in Detail; "loginctl enable-linger" keeps it running without a session.
// launchd has no equivalent: an agent runs only while its user is logged in
// to the GUI, and a service that must run without a login is a LaunchDaemon,
// ScopeSystem.
//
// # Install and updates
//
// systemd: Install writes the unit, reloads the manager, enables the unit and
// restarts it. It restarts rather than "enable --now" because the latter
// leaves a running service on its old unit and binary; a restart starts a
// stopped service and replaces a running one, so Install also applies an
// update.
//
// launchd: Install writes the property list, enables the job, boots out any
// loaded copy, waits until launchd has unloaded it and bootstraps the property
// list again. That is the launchd form of enable and restart: a changed
// property list or binary takes effect, and a stopped job starts. A bootstrap
// that fails leaves the property list installed and the job not loaded; Start
// loads it.
//
// windows: Install creates the service, or rewrites the configuration of the
// registered one, and sets one recovery action, a restart after 5 seconds,
// with the flag that makes it apply to a stop with a non-zero exit code as
// well as to a crash. It writes Env to the Environment value, or deletes that
// value when Env is empty, and then restarts the service: a running copy is
// stopped first, so that the new configuration and binary take effect. A name
// still held by a service marked for deletion is waited out.
//
// No driver creates the account of Config.User; the systemd and launchd
// drivers fail on a missing account before anything is written.
//
// # Start, Stop and restarts
//
// systemd starts and stops the unit with systemctl start and stop; the unit
// stays enabled. It is Restart=always with RestartSec=5.
//
// launchd: Stop boots the job out, unloading it, because KeepAlive relaunches
// a process that is merely killed; the job stays installed and enabled, so it
// loads again at the next boot or login. Start kickstarts a loaded job and
// bootstraps the property list of one that is not loaded. The job is
// KeepAlive=true with ThrottleInterval=5, the counterpart of RestartSec=5, so
// launchd relaunches it on every exit, including the exit status 0 of a
// self-update.
//
// windows: the SCM does not relaunch a service that stops with exit code 0,
// so a restart requested by a Runner is a non-zero service exit, which the
// recovery action restarts (ADR-0034,
// a-requested-restart-on-windows-is-a-non-zero-service-exit.md). Stop stops
// the service and waits until it is stopped; a service stopped on request
// exits 0 and is not restarted. A stop that finds the service stopped with
// the restart exit code, inside its recovery delay, outlasts that delay: the
// SCM gives no time of the stop, so the driver watches the service for the
// whole delay and a margin from the moment it sees it, and stops the service
// the recovery restart starts in that time. Stop, and the stop inside Install
// and Uninstall, then take up to that long, and the service may run briefly
// before it stops again. A stop the service cannot take yet, while it is
// starting, is sent again until it does. Uninstall stops the service, deletes
// it and returns once the SCM no longer holds it; the registry key goes with
// it, and the service's data and account stay.
//
// # Status
//
// systemd: Status reads one "systemctl show" and names journalctl in the
// Detail of a failed unit.
//
// launchd: Status reads "launchctl print" for the job and "launchctl
// print-disabled" for its domain, which says whether it is enabled. A property
// list launchd has not loaded is StateStopped. The Detail of a failed job
// names its log file, or both when the streams go to different files.
//
// windows: Status reads the SCM's state of the service. A service stopped
// with the restart exit code is inside its recovery delay and is StateStarting;
// one stopped with any other non-zero exit code is StateFailed, with the code in
// Detail. Restarts is always 0, as the SCM keeps no restart count.
package service

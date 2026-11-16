// Package daemoncmd provides the commands a tool built on cli/spec uses to manage its daemon.
//
// [NewStatus] returns the "status" command, an HTTP client of the "/status" route that
// (*daemon.Health).Handler serves. The consumer supplies the base URL through a [Resolver]; the
// command requests the snapshot, decodes it into a [daemon.Snapshot] and prints it to the Stdout
// of the execution's [spec.Env].
//
// # Text
//
// By default the command writes two tables separated by a blank line: the overall status, with
// whether the daemon is live and ready and its uptime, then one row per check with its name, its
// kinds, pass or fail, how long it ran and, for a failing check, its error:
//
//	STATUS     LIVE  READY  UPTIME
//	⚠ unready  true  false  1h2m3s
//
//	CHECK     KINDS               STATUS  DURATION  ERROR
//	database  readiness           ✗ fail        2s  daemon: health check timed out after 2s
//	loop      liveness,readiness  ✓ pass       3ms
//
// # JSON
//
// When [spec.Output.Format] is [spec.FormatJSON], as it is under the App's --json or --output
// json, the command writes the snapshot with [spec.WriteJSON] at schema 1, the same bytes the
// daemon serves at "/status". The command declares no --json flag of its own.
//
// # Exit
//
// The command returns nil only when the snapshot is ok. An unready or down snapshot is printed
// all the same, and the command returns [ErrNotOK] wrapped in [spec.Reported], so the App adds
// no message of its own and exits 1. A daemon the command cannot reach, an answer other than
// 200 and a body that is not the schema 1 envelope are ordinary errors: the command prints
// nothing to Stdout and the App reports the error on Stderr.
//
// # Service commands
//
// [NewService] returns the "service" command, which groups install, uninstall, start, stop,
// restart and status; [NewInstall], [NewUninstall], [NewStart], [NewStop], [NewRestart] and
// [NewServiceStatus] return the same commands for a tool that mounts them flat. Each acts on the
// [service.Manager] a [ManagerFunc] returns, called once per run, so the tool builds it with
// [service.New] from the invocation. A tool that mounts [NewServiceStatus] beside [NewStatus]
// sets its Name, since both are named "status".
//
// install, uninstall, start, stop and restart call [service.Manager.CheckPrivileges] before
// anything else, so an unprivileged run changes nothing. install then runs
// [service.Manager.Prepare], the [WithBeforeInstall] hooks, [service.Manager.Install], which
// starts the service or restarts it on the new configuration, the [WithAfterInstall] hooks and
// [service.Manager.Status]. After-install hooks therefore run against a running daemon, and
// whatever the daemon needs when it starts is written by a before-install hook. Running install
// again updates the service in place, so a hook that creates something keeps what a previous run
// created. A secret the daemon needs, such as an API token, goes in a file only the service's
// account may read, named in [service.Config.Args]: [service.Config.Env] is readable by every
// local account. [WithInstallFlags] declares flags on install, which the [ManagerFunc] and the
// hooks read. Every [ServiceOption] is install-only; the other commands accept and ignore them.
//
// status prints the [service.Status] as a table of field and value, or, when the output is JSON,
// with [spec.WriteJSON] at schema 1. install prints the same after installing, and a success
// message on Stderr; uninstall, start, stop and restart print nothing on Stdout and a success
// message on Stderr. uninstall leaves the service's data, account and logs in place.
//
// status exits 0 whatever the state, not_installed included, and 1 only when the state cannot be
// read. uninstall of a service that is not installed says there is nothing to remove and exits 0;
// start, stop and restart of one return an error wrapping [service.ErrNotInstalled] that says to
// install it first. install returns the error of a failed step and rolls nothing back: a failing
// before-install hook leaves nothing registered, a failing after-install hook leaves the service
// installed, and a status that cannot be read afterwards is a warning, with exit 0. Every
// command, status included, turns a [ManagerFunc] error matching [service.ErrContainer] into one
// error saying service management is unavailable in a container and what to do instead
// ([service.ContainerError.Instead]); every other error is returned unchanged.
package daemoncmd

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
// kinds, pass or fail, how long it ran and, for a failing check, its error. A snapshot that
// carries an update adds, between the two, a table of field and value with an "update" row for
// its state and, when the update has an error, an "update error" row; a snapshot without an update
// has no such table:
//
//	STATUS     LIVE  READY  UPTIME
//	⚠ unready  true  false  1h2m3s
//
//	FIELD         VALUE
//	update        pending
//	update error  first boot exited 1
//
//	CHECK     KINDS               STATUS  DURATION  ERROR
//	database  readiness           ✗ fail        2s  daemon: health check timed out after 2s
//	loop      liveness,readiness  ✓ pass       3ms
//
// # JSON
//
// When [spec.Output.Format] is [spec.FormatJSON], as it is under the App's --json or --output
// json, the command writes the snapshot with [spec.WriteJSON] at schema 1, the same bytes the
// daemon serves at "/status", including its "update" object when the daemon reports one. The
// command declares no --json flag of its own.
//
// # Exit
//
// The command returns nil only when the snapshot is ok. An unready or down snapshot is printed
// all the same, and the command returns [ErrNotOK] wrapped in [spec.Reported], so the App adds
// no message of its own and exits 1. A daemon the command cannot reach, an answer other than
// 200 and a body that is not the schema 1 envelope are ordinary errors: the command prints
// nothing to Stdout and the App reports the error on Stderr. The update state never changes the
// exit code.
//
// # update-status
//
// [NewUpdateStatus] returns the "update-status" command, which a tool mounts beside "status". It
// prints where the daemon's self-update stands: the "update" object of the daemon's "/status",
// asked through the same [Resolver] and [StatusOption]s as [NewStatus]. When the daemon gives no
// update, because it cannot be resolved or reached, the request times out, it answers other than
// 200 or with a body that is not the schema 1 envelope, or its snapshot carries no update, the
// command asks the fallback [daemon.UpdateReporter] instead, typically one reading the state the
// self-update keeps on disk. A cancelled context is not such a failure: the command returns its
// error without asking the fallback.
//
// # update-status text
//
// The command writes a table of field and value: the source the status was read from, its state,
// then each other field of the update that is set:
//
//	FIELD          VALUE
//	source         daemon
//	status         pending
//	from version   v1.4.0
//	to version     v1.5.0
//	boot attempts  2
//	started at     2026-11-04T03:20:00Z
//
// A reachable daemon that reports no update, with no fallback, prints "self-update is not
// configured". When a failure of the daemon makes the command read the fallback, it notes the
// failure on Stderr as a warning, which --quiet drops, so Stdout carries the status alone.
//
// # update-status JSON
//
// The command writes one flat object with [spec.WriteJSON] at schema 1: "source", which is
// "daemon", "store" for the fallback or "none" for a daemon that reports no update with no
// fallback, then the keys of [daemon.UpdateStatus], none for "none", then "daemon_error", the
// failure of the daemon that made the command read the fallback. daemon_error is absent when the
// daemon answered without an update. The Stderr warning is written in this mode too, unless quiet.
//
// # update-status exit
//
// The command returns nil for an update that is idle, pending or confirmed, and for source none.
// An update that is rolled_back or unknown, or that carries an error, is printed all the same,
// and the command returns [ErrUpdateNotOK] wrapped in [spec.Reported], so the App adds no message
// and exits 1. A rolled-back update therefore keeps update-status exiting 1 until the next update
// begins. A daemon that fails with no fallback is an ordinary error: the command prints nothing to
// Stdout and the App reports the error on Stderr.
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
//
// # Update
//
// [NewUpdate] returns the update command of a daemon: the command of
// [go.vipengele.org/update/updatecmd], guarded by a [selfupdate.Machine] so an update is refused
// while an earlier one is pending. Each installed update is recorded as pending with
// [selfupdate.Machine.Begin], and rolled back when that fails, so the daemon's next boots judge
// the new build and confirm it or restore the previous binary. The command then restarts the
// service when it runs the installed binary and is running, and waits until it runs again; a
// stopped, starting, failed or unknown service is left alone with a message, and a binary no
// service manager runs is reported as one nothing restarts. Only a restarted service that does not
// come up fails the command once the update is recorded.
package daemoncmd

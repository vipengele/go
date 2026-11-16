// Package selfupdate tracks a daemon's update of its own binary, so that an update the new build
// cannot confirm is rolled back, whichever process notices.
//
// An update moves through four statuses, kept in a Record in a Store: idle, with no update in
// progress; pending, once the new binary is installed and its backup kept; confirmed, once the
// updated build has proved itself and the backup is released; and rolled_back, once the backup is
// restored. The steps run in different processes (the one that installs, each boot of the new
// build, a guard that judges it), so the Store changes the Record only under a lock.
//
// Each boot of the updated build while the update is pending is counted in the Record before the
// build is judged, so a build that crashes before it can confirm still uses up its attempts. A
// rollback restores the backup binary over the target before it records rolled_back: a record
// never says rolled back while the new binary is still in place.
//
// Machine.Apply installs a release through go.vipengele.org/update and writes the Record pending
// before the executable is swapped: Machine.Begin runs from an update.BeforeInstall hook of
// update.Config.Apply, once the staged binary has passed every check and before Install renames
// anything, and the Runner's update check installs through Machine.Apply too. A crash between the
// record and the swap leaves a pending update whose binary never started, which the next Boot judges
// and rolls back. When Begin refuses, with ErrUpdatePending while another update is pending, nothing
// is installed. When the install fails after Begin and the executable is as it was,
// Machine.Abandon records the update rolled_back at once, so a later attempt may retry. It refuses a
// record that is not pending with ErrNoPendingUpdate and an update pending for the running version
// with ErrUpdateRunning, since the swap happened and Boot judges it. When the executable may hold no
// binary (an *update.RestoreError), the record stays pending for Boot.
//
// Machine.Apply gives the Config a BackupGuard of its own call, which answers as the Machine, so no
// update starts while another is pending, until that call's Begin has succeeded, and from then on
// does not count the pending record the call began, so its own install is not refused. Every other
// Config carrying the Machine stays refused while the update is pending. On success Machine.Apply
// runs the post-update hooks, keeps the backup update made, and returns an error wrapping
// daemon.ErrRestartRequired. A Runner that returns that error ends Supervisor.Run with a restart,
// and the service supervisor starts the installed binary, whose boots Boot judges. A version is
// compared with a leading "v" ignored, so a build reporting "1.2.3" is the release tagged "v1.2.3".
//
// NewRunner returns the Runner that schedules the checks and applies updates, and implements
// daemon.Reloader[T]. A daemon passes it to the Supervisor with daemon.WithReload, so a SIGHUP loads
// the configuration and the Runner takes its Settings from it through the settingsOf function:
//
//	runner, err := selfupdate.NewRunner(cfg, machine, selfupdate.DefaultSettings(),
//		func(c Config) selfupdate.Settings { return c.Update })
//	sup, err := daemon.New([]daemon.Runner{runner}, daemon.WithReload(loadConfig))
//
// Settings holds two configuration keys: interval, the time between checks, which is
// DefaultInterval (24h) when zero and at least MinInterval (1h) otherwise; and auto_apply, which
// makes a check install the release it finds rather than only report it. DefaultSettings sets the
// default interval and auto_apply on. The first check waits for a jitter, a hash of the binary's
// path modulo a tenth of the interval, so daemons at different paths do not all ask the release
// server at once. On Windows there is no SIGHUP, so a change to interval or auto_apply does not
// reload there; it takes effect at the next start.
//
// The Machine is the update.BackupGuard of the binary: every update.Config the consumer uses for it,
// such as the one behind its update command and its update check hooks, must carry the Machine as
// its BackupGuard, or the check hook on Windows removes the backup of an update awaiting
// confirmation (ADR-0033). NewRunner refuses a cfg whose BackupGuard is another value.
//
// A check that installs an update ends the Runner with an error matching daemon.ErrRestartRequired,
// which the Supervisor turns into a restart request. The old binary's backup is kept for the
// Machine's rollback, so the caller never calls Installed.Discard: that would remove the backup a
// failed boot of the new build restores.
//
// FileStore is the Store over one JSON file. It creates no directory, and a missing file is idle.
// A corrupt file is an error rather than idle, so that a pending update is never forgotten; an
// operator clears it by deleting the file.
//
// Reporter reports the recorded update as a daemon.UpdateStatus for the daemon's health snapshot.
// It only reads the Store: a missing record is idle, and a record that cannot be read is unknown
// with the cause in Error, never idle. The consumer supplies the release channel and watermark as
// plain strings, so the package knows nothing of where releases come from.
package selfupdate

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
// Machine.Apply installs a release through go.vipengele.org/update with the Machine as the
// Config's BackupGuard, so no update starts while another is pending, records the update pending
// with the backup update keeps, and returns an error wrapping daemon.ErrRestartRequired. A Runner
// that returns that error ends Supervisor.Run with a restart, and the service supervisor starts the
// installed binary, whose boots Boot judges. A record that cannot be stored rolls the install back.
// A version is compared with a leading "v" ignored, so a build reporting "1.2.3" is the release
// tagged "v1.2.3".
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

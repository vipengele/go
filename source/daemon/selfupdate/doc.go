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
// FileStore is the Store over one JSON file. It creates no directory, and a missing file is idle.
// A corrupt file is an error rather than idle, so that a pending update is never forgotten; an
// operator clears it by deleting the file.
package selfupdate

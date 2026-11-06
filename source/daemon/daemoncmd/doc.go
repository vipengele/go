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
package daemoncmd

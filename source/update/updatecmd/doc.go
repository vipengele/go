// Package updatecmd provides the `update` command of a tool built on
// [go.vipengele.org/update], as a framework-neutral [go.vipengele.org/cli/spec.Command] that a
// tool's main adds next to its own, one [go.vipengele.org/cli/core.App.Add] call per command:
//
//	for _, cmd := range updatecmd.Commands(cfg) {
//		app.Add(cmd)
//	}
//
// The command replaces the running executable with a release from the Config's Source. Without
// flags it updates to the latest release, after asking for confirmation; --yes skips the question,
// which a run whose standard input is not a terminal cannot answer, and which then fails with a
// [go.vipengele.org/cli/spec.UsageError]. --to installs the named release instead, older ones
// included. --check reports whether a newer release exists without installing anything, and exits
// [ExitUpdateAvailable] when one does, so a script can tell the two answers apart.
//
// A release below the highest version the tool has applied or is running is a downgrade, and the
// command refuses it before it asks anything, whether it is the latest release or one named by --to;
// only --allow-downgrade lifts that, --to alone does not. --check treats such a latest release as
// up to date.
//
// Releases come in channels that accumulate: stable, then beta, then edge, where a follower of one
// receives the releases of the ones before it. --channel follows another channel for one run in
// place of the Config's. Switching to a lower channel never downgrades: a stable tool running
// v1.3.0-beta.2 stays there until a stable release sorts above it.
//
// A development build is refused in every mode with the [*go.vipengele.org/update.DevBuildError]
// of [go.vipengele.org/update.Config.CheckRelease]. An install an external manager owns is refused
// with the [*go.vipengele.org/update.ManagedInstallError] of
// [go.vipengele.org/update.Config.CheckManaged] before any request is made; --check still reports a
// newer release for it, and says what to run instead.
//
// The result of --check is data, written to the standard output; every other line is a message on
// the standard error. The command carries the [go.vipengele.org/cli/spec.SkipUpdateCheck]
// annotation, so the background update check never runs around it.
package updatecmd

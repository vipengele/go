// Package cli is the root of the vipengele libraries for building command-line tools. It runs
// commands described with go.vipengele.org/cli/spec.
//
// A tool builds an [App] in its main from its [spec.Identity] and the [Option]s it wants, adds
// its commands, each a [spec.Command] tree, and runs them with [App.Execute], which reads the
// arguments, standard streams, working directory and environment variables of the process and
// returns the exit code:
//
//	func main() {
//		app := cli.New(identity, cli.WithVerbose(), cli.WithJSON(), cli.WithPreRun(checkForUpdate))
//		app.Add(newGreet())
//		os.Exit(app.Execute())
//	}
//
// The App's root command is named by the identity's Name and holds the added commands as its
// subcommands; it has no run function, so given no argument it writes its help. [Execute] runs
// a root command the tool builds itself instead.
//
// The App owns the "version" command of the versioncmd package: every root it runs holds it,
// printing the build information of the running binary, so every tool answers "version --json"
// with the JSON contract the buildinfo package specifies. A tool does not add "version"; a
// command of that name it adds makes the tree invalid.
//
// The standard flags are opt-in: [WithVerbose], [WithQuiet], [WithNoColor] and [WithJSON] each
// declare one inherited flag on the root. A run function reads them, together with the
// environment's colour settings, from the [spec.Output] of its [spec.Invocation], never from a
// flag handle. Without [WithJSON], --json is a local flag of "version" alone; with it, "version"
// reads the standard flag.
//
// Hooks run around every run function: [WithPreRun] adds a [PreRunHook] called before it and
// [WithPostRun] a [PostRunHook] called after it. A hook receives the [spec.CommandInfo] of the
// command that runs, whose annotations, the Annotations of its [spec.Command], let a hook skip
// a command: an update check skips one annotated with [spec.SkipUpdateCheck], as "version" is.
// This package does not import the update project; the tool's main wires its update check as a
// hook.
//
// [App.ExecuteArgs] and [ExecuteArgs] run the same commands against a [spec.Env] and arguments
// the caller gives, and touch nothing else of the process, so a test runs the tool's commands
// against buffers, a fixed environment and a temporary working directory.
//
// Every execution checks the tree with [spec.Command.Validate] before running it, writes every
// error to the standard error as "<name>: <error>", where name is the root command's Name, and
// returns [ExitOK], [ExitError], [ExitUsage], or [ExitInterrupted] for a run error after an
// interrupt and for a run abandoned on a second interrupt. Each command answers --help by
// writing its help to the standard output. The commands are run with github.com/spf13/cobra,
// which no API of this package exposes: a tool's commands depend on spec alone.
package cli

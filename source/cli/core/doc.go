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
// Every execution checks the tree with [spec.Command.Validate] before running it and writes every
// error to the standard error as "<name>: <error>", where name is the root command's Name, unless
// the error carries a [spec.ReportedError]. Each command answers --help by writing its help to the
// standard output. The commands are run with github.com/spf13/cobra, which no API of this package
// exposes: a tool's commands depend on spec alone.
//
// # Exit codes
//
// An execution returns one of these codes:
//
//	0        success, and --help ([ExitOK])
//	1        a general error ([ExitError])
//	2        a usage error: a command line the tree does not accept ([ExitUsage])
//	3-125    registrable: a tool gives them to its own errors
//	130      interrupted ([ExitInterrupted])
//
// 0, 2 and 130 are reserved for the runs they describe, and no error can be given them. 1 can also
// be given to an error by registration. 125 is the highest registrable code, because a shell
// reports 126 and above for its own failures and a code above 255 does not survive the exit
// status.
//
// The exit code of an error is, first match wins:
//  1. 130, with nothing written, when the root context is cancelled and the error is
//     [context.Canceled]. A run function that cancels or times out a context it derived itself
//     while the root context is live is not interrupted;
//  2. the code of a [spec.CodedError] the error carries, when it is 1 or 3 through 125; any other
//     code is ignored;
//  3. the code of the first match among the [WithExitCode] and [WithExitCodeAs] registrations, in
//     the order the options were given, also for an error that is a join of several;
//  4. 2 for a usage error;
//  5. 1 otherwise.
//
// The context an [App] and the package-level [Execute] run with is never cancelled, so rule 1
// does not apply to them and no execution through this package's API returns 130.
//
// The registrations belong to the [App] and are read-only after [New]. The package-level
// [Execute] and [ExecuteArgs] have none, so for them rules 1, 2, 4 and 5 apply.
//
// [spec.Reported] suppresses the "<name>: <error>" line and nothing else: the code is still chosen
// by rules 2 to 5, so a reported usage error exits 2 silently. The "invalid command tree" message
// is never suppressed.
//
// A [PreRunHook] error is mapped as a run function's error is. A [PostRunHook] error after a run
// that succeeded is opaque: it is written and exits 1, whatever it wraps. When the run function
// fails, its error decides, whatever a post-run hook returns.
package cli

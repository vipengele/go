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
// The standard flags are opt-in: [WithVerbose], [WithQuiet], [WithNoColor], [WithJSON] and
// [WithOutput] each declare one inherited flag on the root. A run function reads them, together
// with whether colour is allowed on each of the standard output and the standard error, from the
// [spec.Output] of its [spec.Invocation], never from a flag handle. Without [WithJSON], --json is
// a local flag of "version" alone; with it, "version" reads the standard flag.
//
// The format of the output, [spec.Output.Format], is text unless the command line asks for JSON.
// An App takes one of two standard flags for it, and [New] panics when it is given both:
// [WithJSON]'s --json, true for JSON, or [WithOutput]'s --output, text or json. Under --output, a
// command that declares its own --json, as "version" does, reads that --json together with the
// Format, and the two must agree: --output text with a --json that is true, --output json with
// --json=false, and any --output other than text or json are usage errors. A flag given twice
// takes its last value.
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
// error to the standard error, unless the error carries a [spec.ReportedError], as a tree headed
// "<name>: ", where name is the root command's Name: each layer of a wrapped error is a row
// marked "→" and each error of an [errors.Join] a row marked "•", indented under the layer it is
// joined beneath, as [ExecuteArgs] describes. An error that neither wraps nor joins prints as
// "<name>: <error>". [WithErrorRenderer] renders an error of a type the tool chooses, and what it
// wraps, in the tool's own way. Each command answers --help by writing its help to the
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
// The root context is cancelled by an interrupt. The first value [spec.Env.Interrupts] delivers
// cancels the context every hook and run function receives, and does nothing else: the run goes
// on, and the error it returns is mapped by the rules above, so [context.Canceled] exits 130 with
// nothing written and any other error is written and keeps the code rules 2 to 5 give it. The
// second value returns 130 at once, without waiting for the run, which is abandoned. [Execute] and
// [App.Execute] deliver SIGINT and SIGTERM as those values.
//
// The registrations belong to the [App] and are read-only after [New]. The package-level
// [Execute] and [ExecuteArgs] have none, so for them rules 1, 2, 4 and 5 apply.
//
// [spec.Reported] suppresses the written error and nothing else: the code is still chosen
// by rules 2 to 5, so a reported usage error exits 2 silently. The "invalid command tree" message,
// one row per problem [spec.Command.Validate] finds, is never suppressed.
//
// A [PreRunHook] error is mapped as a run function's error is. A [PostRunHook] error after a run
// that succeeded is opaque: it is written and exits 1, whatever it wraps. When the run function
// fails, its error decides, whatever a post-run hook returns.
package cli

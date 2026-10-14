// Package cli is the root of the vipengele libraries for building command-line tools. It runs
// commands described with go.vipengele.org/cli/spec.
//
// A tool builds an [App] in its main from its [spec.Identity], adds its commands, each a
// [spec.Command] tree, and runs them with [App.Execute], which reads the arguments, standard
// streams, working directory and environment variables of the process and returns the exit
// code:
//
//	func main() {
//		app := cli.New(identity)
//		app.Add(newGreet())
//		app.Add(versioncmd.New(buildinfo.Read()))
//		os.Exit(app.Execute())
//	}
//
// The App's root command is named by the identity's Name and holds the added commands as its
// subcommands; it has no run function, so given no argument it writes its help. [Execute] runs
// a root command the tool builds itself instead.
//
// [App.ExecuteArgs] and [ExecuteArgs] run the same commands against a [spec.Env] and arguments
// the caller gives, and touch nothing else of the process, so a test runs the tool's commands
// against buffers, a fixed environment and a temporary working directory.
//
// Every execution checks the tree with [spec.Command.Validate] before running it, writes every
// error to the standard error as "<name>: <error>", where name is the root command's Name, and
// returns [ExitOK], [ExitError] or [ExitUsage]. Each command answers --help by writing its help
// to the standard output. The commands are run with github.com/spf13/cobra, which no API of this
// package exposes: a tool's commands depend on spec alone.
package cli

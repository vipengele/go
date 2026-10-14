// Package cli is the root of the vipengele libraries for building command-line tools. It runs a
// tree of commands described with go.vipengele.org/cli/spec.
//
// A tool describes its commands as a [spec.Command] tree and runs it from main with [Execute],
// which reads the arguments, standard streams, working directory and environment variables of
// the process and returns the exit code:
//
//	func main() {
//		os.Exit(cli.Execute(newRoot()))
//	}
//
// [ExecuteArgs] runs the same tree against a [spec.Env] and arguments the caller gives, and
// touches nothing else of the process, so a test runs the tool's commands against buffers, a
// fixed environment and a temporary working directory.
//
// Both check the tree with [spec.Command.Validate] before running it, write every error to the
// standard error as "<name>: <error>", where name is the root command's Name, and return
// [ExitOK], [ExitError] or [ExitUsage]. Each command answers --help by writing its help to the
// standard output. The commands are run with github.com/spf13/cobra, which no API of this
// package exposes: a tool's commands depend on spec alone.
package cli

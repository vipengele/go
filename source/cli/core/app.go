package cli

import "go.vipengele.org/cli/spec"

// App is the shell of one tool: its [spec.Identity] and the commands it runs. A tool builds one
// in its main with [New], adds its commands with [App.Add] and runs them with [App.Execute]:
//
//	func main() {
//		app := cli.New(identity)
//		app.Add(newGreet())
//		os.Exit(app.Execute())
//	}
//
// Every execution assembles its own root command, named by the identity's Name and holding the
// added commands as its subcommands, and leaves the added commands as they are, so a built App
// may run any number of times, concurrently. Adding a command while the App runs is not safe.
type App struct {
	identity spec.Identity
	settings settings
	commands []*spec.Command
}

// settings holds what the [Option]s given to [New] set.
type settings struct{}

// Option configures an [App] when [New] builds it.
type Option func(*settings)

// New returns an App for the tool identity names, configured by opts in order. It checks
// nothing: the root command and the commands under it are checked on every execution.
func New(identity spec.Identity, opts ...Option) *App {
	a := &App{identity: identity}
	for _, opt := range opts {
		opt(&a.settings)
	}
	return a
}

// Add adds cmd as a subcommand of the root, after the commands added before it. It checks
// nothing: a nil command, an invalid one, or a name another added command already has is
// reported by [App.ExecuteArgs] as an invalid command tree.
func (a *App) Add(cmd *spec.Command) {
	a.commands = append(a.commands, cmd)
}

// Execute runs the App with the arguments of the process, os.Args[1:], against the environment
// of the process: its standard streams, working directory and environment variables. It
// returns the exit code, which a tool's main passes to os.Exit. It behaves as [App.ExecuteArgs]
// does, and returns [ExitError] after writing "<name>: <error>" to os.Stderr when the working
// directory cannot be determined.
func (a *App) Execute() int {
	return executeProcess(prefix(a.root()), a.ExecuteArgs)
}

// ExecuteArgs runs the App with args against env, and returns the exit code, as the
// package-level [ExecuteArgs] runs the App's root command: a root named by the identity's Name,
// with no run function of its own and the added commands, in the order they were added, as its
// subcommands. Given no argument, the root writes its help to env.Stdout and returns [ExitOK].
// The root is checked on every call, so an App with no command, or with two commands of one
// name, returns [ExitError] after writing "<name>: invalid command tree: <error>" to
// env.Stderr.
func (a *App) ExecuteArgs(env *spec.Env, args []string) int {
	return ExecuteArgs(env, a.root(), args)
}

// root returns a fresh root command for one execution. Its Commands is a copy of the added
// commands, so no execution shares a slice the App appends to.
func (a *App) root() *spec.Command {
	return &spec.Command{
		Name:     a.identity.Name,
		Commands: append([]*spec.Command(nil), a.commands...),
	}
}

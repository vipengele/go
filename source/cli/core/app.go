package cli

import (
	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/core/versioncmd"
	"go.vipengele.org/cli/spec"
)

// App is the shell of one tool: its [spec.Identity] and the commands it runs. A tool builds one
// in its main with [New] and the [Option]s it wants, adds its commands with [App.Add] and runs
// them with [App.Execute]:
//
//	func main() {
//		app := cli.New(identity, cli.WithJSON())
//		app.Add(newGreet())
//		os.Exit(app.Execute())
//	}
//
// The App owns the "version" command, which prints its build information as the versioncmd
// package describes: every root it runs holds it after the added commands, so every tool
// answers "version --json". A tool does not add a command named "version"; one it adds makes
// the tree invalid.
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
type settings struct {
	// verbose, quiet, noColor and json opt the App into the standard flag of the same name.
	verbose bool
	quiet   bool
	noColor bool
	json    bool
	// preRun and postRun are the hooks [WithPreRun] and [WithPostRun] add, in the order they
	// were added. Nothing changes them after [New], so concurrent executions share them.
	preRun  []PreRunHook
	postRun []PostRunHook
	// buildInfo is what the "version" command prints: the Info [WithBuildInfo] gives, or
	// [buildinfo.Read] when no option gives one.
	buildInfo *buildinfo.Info
}

// Option configures an [App] when [New] builds it.
type Option func(*settings)

// New returns an App for the tool identity names, configured by opts in order. It checks
// nothing: the root command and the commands under it are checked on every execution. It reads
// the build information of the running binary with [buildinfo.Read] unless [WithBuildInfo]
// gives it.
func New(identity spec.Identity, opts ...Option) *App {
	a := &App{identity: identity}
	for _, opt := range opts {
		opt(&a.settings)
	}
	if a.settings.buildInfo == nil {
		info := buildinfo.Read()
		a.settings.buildInfo = &info
	}
	return a
}

// WithBuildInfo makes the App's "version" command print info instead of the build information
// [buildinfo.Read] reports for the running binary.
func WithBuildInfo(info buildinfo.Info) Option {
	return func(s *settings) { s.buildInfo = &info }
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
	return executeProcess(prefix(a.root(standardFlags{})), a.ExecuteArgs)
}

// ExecuteArgs runs the App with args against env, and returns the exit code, as the
// package-level [ExecuteArgs] runs the App's root command: a root named by the identity's Name,
// with no run function of its own and the added commands, in the order they were added, as its
// subcommands followed by the "version" command, and the standard flags the App's options opt
// into as its inherited flags. Given no argument, the root writes its help to env.Stdout and
// returns [ExitOK].
// The root is checked on every call, so an App with two commands of one name, "version"
// included, returns [ExitError] after writing "<name>: invalid command tree: <error>" to
// env.Stderr. So does a command that declares a flag with the name or the shorthand of a
// standard flag the App takes.
//
// "version --json" writes JSON whether or not the App takes the standard --json. Without it,
// --json is a local flag of "version"; with it, "version" declares no flag of its own and reads
// the standard one, given before or after "version".
//
// Every run function receives a [spec.Invocation] whose Output reports the standard flags the
// command line sets; a standard flag the App does not take reads as unset. --verbose with
// --quiet returns [ExitUsage] and calls no run function. The Output forbids colour when
// --no-color is set, when the environment variable NO_COLOR or <EnvPrefix>_NO_COLOR, named by
// the identity, is set and not empty, or when env.Stdout is not a terminal as
// [spec.Env.IsTerminal] reports it for [spec.Stdout].
//
// The hooks [WithPreRun] and [WithPostRun] add run around every run function it calls, as they
// describe.
func (a *App) ExecuteArgs(env *spec.Env, args []string) int {
	flags := newStandardFlags(a.settings)
	resolver := outputResolver{identity: &a.identity, flags: flags}
	pre, post := a.settings.hookFuncs()
	cfg := cobra.Config{Output: resolver.resolve, PreRun: pre, PostRun: post}
	return executeArgs(env, a.root(flags), args, cfg)
}

// root returns a fresh root command for one execution, declaring flags as its inherited flags.
// Its Commands is a copy of the added commands followed by a "version" command of its own, so
// no execution shares a slice the App appends to or a flag handle another execution reads.
func (a *App) root(flags standardFlags) *spec.Command {
	return &spec.Command{
		Name:     a.identity.Name,
		Flags:    flags.list(),
		Commands: append(append([]*spec.Command(nil), a.commands...), a.version(flags)),
	}
}

// version returns the "version" command for one execution. It declares a local --json unless
// flags holds the standard --json, which it then reads through the invocation's Output.
func (a *App) version(flags standardFlags) *spec.Command {
	var opts []versioncmd.Option
	if flags.json != nil {
		opts = append(opts, versioncmd.WithOutputJSON())
	}
	return versioncmd.New(*a.settings.buildInfo, opts...)
}

// Package cobra runs a [spec.Command] tree with github.com/spf13/cobra. It is the only package of
// the module that imports cobra, so replacing the library changes this package and nothing that
// builds commands.
//
// [Run] builds a fresh cobra tree for every execution. Cobra keeps parsed flag values in its
// commands, so a tree reused across executions would carry one execution's flags into the next;
// the [spec.Command] tree it is built from holds no such state and is shared freely.
package cobra

import (
	"context"
	"fmt"
	"slices"
	"strings"

	spf "github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"go.vipengele.org/cli/spec"
)

// UsageError reports a command line the tree does not accept: an unknown flag or a flag value
// that does not parse, an unknown subcommand, or a number of positional arguments the command's
// [spec.ArgPolicy] rejects. Every other error [Run] returns comes from a run function, or from
// writing help.
type UsageError struct {
	// Path is the space-separated names of the command that rejected the command line, the
	// root's name left out: "" for the root, "remote add" for a grandchild.
	Path string
	// Err is the problem.
	Err error
}

// Error names the command by its Path, when it is not the root, followed by the problem.
func (e *UsageError) Error() string {
	if e.Path == "" {
		return e.Err.Error()
	}
	return e.Path + ": " + e.Err.Error()
}

// Unwrap returns the problem, so errors.As finds a [*spec.ArgCountError] through a UsageError.
func (e *UsageError) Unwrap() error {
	return e.Err
}

// Config is what an execution needs beyond the tree, the environment and the arguments. The
// zero value runs every command with the zero [spec.Output].
type Config struct {
	// Output returns the [spec.Output] of the invocation a run function is about to receive,
	// which [Run] sets as the invocation's Output. It is called once per run, after the command
	// line is parsed and before the run function, with the invocation holding the parsed flag
	// values. An error it returns is a problem of the command line: Run returns it as a
	// [*UsageError] and calls no run function. Nil leaves the zero Output.
	Output func(inv *spec.Invocation) (spec.Output, error)
	// PreRun are called in order before the run function, after Output, with the invocation the
	// run function is about to receive and the command that runs. Each returns the context the
	// next PreRun, the run function and every PostRun receive; a nil context keeps the one it
	// was given. The first error stops the run: Run returns it as it is, ignores the context
	// returned with it and calls neither the run function nor any PostRun.
	PreRun []func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error)
	// PostRun are called in reverse order after the run function returns, every one of them,
	// with the error the run function returned. When the run function returned an error, Run
	// returns that error and ignores theirs. Otherwise Run returns the first error a PostRun
	// returns, as a [*HookError], so the error is never taken for a [*UsageError].
	PostRun []func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo, runErr error) error
	// Completion adds cobra's completion command to the root, which writes the completion
	// script of bash, zsh, fish or powershell to env.Stdout. It is cobra's command, not a
	// [spec.Command]: neither Output nor any hook runs around it, so nothing a hook depends on
	// can stop a script from being written. A root with a command named completion of its own
	// keeps that command instead.
	Completion bool
}

// HookError reports an error a [Config] PostRun returned after the run function succeeded. It
// prints as that error and deliberately has no Unwrap: errors.As finds no [*UsageError] through
// it, so a hook's error is never a problem of the command line.
type HookError struct {
	// Err is the error the hook returned.
	Err error
}

// Error returns the hook's error text.
func (e *HookError) Error() string {
	return e.Err.Error()
}

// Run executes root with args against env, configured by cfg, and returns the error of the command line or of the
// run function it selects. root must be valid as [spec.Command.Validate] reports it, and env
// resolved as [spec.Env.Resolved] returns it. A nil args is no arguments; it never falls back
// to the arguments of the process.
//
// The functions of cfg run only around a run function the command line reaches: not for
// --help, not for a command without Run, and not for a command line the tree rejects.
//
// The tree answers --help, on every command, by writing the command's help to env.Stdout and
// returning nil. It has no help subcommand, and a completion subcommand only when cfg.Completion
// is set; it refuses a tree in which a command declares a flag named help. A command with
// subcommands and no Run writes its help to env.Stdout and returns nil when it is given no
// positional argument, and returns a [*UsageError] naming the first one when it is.
//
// Cobra's hidden __complete command, which a completion script runs, is there whatever cfg
// says: it writes the candidates of the [spec.Completion] of the flag or the positional
// arguments being completed, and runs neither Output nor any hook, so nothing a hook writes
// reaches the completing shell.
func Run(ctx context.Context, env *spec.Env, root *spec.Command, args []string, cfg Config) error {
	cmd, err := build(env, cfg, root, nil, nil, completing(args))
	if err != nil {
		return err
	}
	cmd.CompletionOptions.DisableDefaultCmd = !cfg.Completion
	cmd.SetHelpCommand(&spf.Command{Hidden: true})
	cmd.SetFlagErrorFunc(func(c *spf.Command, err error) error {
		return &UsageError{Path: relPath(c), Err: err}
	})
	cmd.SetIn(env.Stdin) // [lydite:exclude_from_mutation][cobra reads no input and the adapter hands Run the Env directly, so the setting only keeps a stray read inside the Env]
	cmd.SetOut(env.Stdout)
	cmd.SetErr(env.Stderr) // [lydite:exclude_from_mutation][errors and usage are silenced, so cobra writes nothing to its error stream; the setting only keeps a stray write inside the Env]
	if args == nil {
		args = []string{}
	}
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

// completing reports whether args request shell completion through cobra's hidden __complete
// command.
func completing(args []string) bool {
	return len(args) > 0 && (args[0] == spf.ShellCompRequestCmd || args[0] == spf.ShellCompNoDescRequestCmd)
}

// build returns the cobra command for c and, recursively, its subcommands. inherited are the
// inherited flags of c's ancestors, which c reads along with its own, and parents the names from
// the root to c's parent. With complete, the flags of c register their completion with cobra.
func build(env *spec.Env, cfg Config, c *spec.Command, inherited []spec.Flag, parents []string,
	complete bool,
) (*spf.Command, error) {
	path := append(slices.Clone(parents), c.Name)
	cmd := &spf.Command{
		Use:           c.Name,
		Short:         c.Summary,
		Long:          c.Description,
		Hidden:        c.Hidden,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	visible := append([]spec.Flag(nil), inherited...)
	children := append([]spec.Flag(nil), inherited...)
	for _, f := range c.Flags {
		if f.Name() == "help" {
			return nil, fmt.Errorf("command %q: flag --help is reserved for help", c.Name)
		}
		fs := cmd.Flags()
		if f.Inherited() {
			fs = cmd.PersistentFlags()
			children = append(children, f)
		}
		if err := define(fs, f); err != nil {
			return nil, fmt.Errorf("command %q: %w", c.Name, err)
		}
		if fn := completionFunc(f.Completion(), nil); complete && fn != nil {
			// Registration fails only for a flag the command lacks or one registered already;
			// define has just created this one and nothing else registers it.
			_ = cmd.RegisterFlagCompletionFunc(f.Name(), fn)
		}
		visible = append(visible, f)
	}
	if c.Run != nil {
		cmd.ValidArgsFunction = completionFunc(c.ArgCompletion, c.Args.Max)
	}

	// Cobra's own --help flag takes the shorthand -h unconditionally, and pflag panics when a
	// flag of the command already holds it, so a command with such a flag declares --help here
	// and gives up -h. Every other command takes cobra's help flag.
	if hasShorthand(visible, "h") {
		cmd.Flags().Bool("help", false, "help for "+c.Name)
	}

	for _, sub := range c.Commands {
		child, err := build(env, cfg, sub, children, path, complete)
		if err != nil {
			return nil, err
		}
		cmd.AddCommand(child)
	}

	// Cobra shows the help of a command without a run function and never checks its arguments,
	// so an unknown subcommand would succeed. A command with no Run runs help itself, after
	// unknownCommand has rejected any argument.
	if c.Run == nil {
		cmd.Args = unknownCommand
		cmd.RunE = func(cmd *spf.Command, _ []string) error {
			return cmd.Help()
		}
		return cmd, nil
	}

	policy, hasCommands, run, annotations := c.Args, len(c.Commands) > 0, c.Run, c.Annotations
	cmd.Args = func(cmd *spf.Command, args []string) error {
		err := policy.Check(args)
		if err == nil {
			return nil
		}
		if limit, bounded := policy.Max(); hasCommands && bounded && len(args) > limit { // [lydite:exclude_from_mutation][Check accepts len(args) == limit, so the boundary is unreachable]
			return unknownCommand(cmd, args)
		}
		return &UsageError{Path: relPath(cmd), Err: err}
	}
	cmd.RunE = func(cmd *spf.Command, args []string) error {
		values, set, err := read(cmd.Flags(), visible)
		if err != nil {
			return err
		}
		inv := spec.NewInvocation(env, args, values, spec.ExplicitFlags(set...))
		if cfg.Output != nil {
			out, err := cfg.Output(inv)
			if err != nil {
				return &UsageError{Path: relPath(cmd), Err: err}
			}
			inv.Output = out
		}
		return runHooked(cmd.Context(), cfg, inv, spec.NewCommandInfo(path, annotations), run)
	}
	return cmd, nil
}

// runHooked calls run with inv between the PreRun and the PostRun of cfg, which receive info,
// and returns the error [Config] describes. The first PreRun receives ctx, and each later call
// the context the last PreRun returned that was not nil.
func runHooked(ctx context.Context, cfg Config, inv *spec.Invocation, info spec.CommandInfo,
	run func(context.Context, *spec.Invocation) error,
) error {
	for _, pre := range cfg.PreRun {
		next, err := pre(ctx, inv, info)
		if err != nil {
			return err
		}
		if next != nil {
			ctx = next
		}
	}
	runErr := run(ctx, inv)
	var hookErr error
	for _, post := range slices.Backward(cfg.PostRun) {
		if err := post(ctx, inv, info, runErr); err != nil && hookErr == nil {
			hookErr = err
		}
	}
	if runErr != nil || hookErr == nil {
		return runErr
	}
	return &HookError{Err: hookErr}
}

// completionFunc returns the cobra completion function of c, and nil for a [spec.NoCompletion].
// limit, when not nil, is the most positional arguments the completed command accepts: once the
// command line holds that many, nothing more is proposed.
//
// Cobra keeps every flag's completion function in a process-wide map it never clears, keyed by
// the flag, and [Run] builds new flags on every execution, so build registers flag completion
// only when the command line requests completion; registering on every execution would grow
// the map for as long as the process runs.
func completionFunc(c spec.Completion, limit func() (int, bool)) spf.CompletionFunc {
	kind := c.Kind()
	if kind == spec.NoCompletion {
		return nil
	}
	choices, exts, fn := c.Choices(), c.Extensions(), c.Func()
	return func(cmd *spf.Command, args []string, toComplete string) ([]spf.Completion, spf.ShellCompDirective) {
		if limit != nil {
			if n, bounded := limit(); bounded && len(args) >= n {
				return nil, spf.ShellCompDirectiveNoFileComp
			}
		}
		switch kind {
		case spec.ChoicesCompletion:
			var out []spf.Completion
			for _, v := range choices {
				if strings.HasPrefix(v, toComplete) {
					out = append(out, v)
				}
			}
			return out, spf.ShellCompDirectiveNoFileComp
		case spec.FilesCompletion:
			if len(exts) == 0 {
				return nil, spf.ShellCompDirectiveDefault
			}
			return exts, spf.ShellCompDirectiveFilterFileExt
		case spec.DirsCompletion:
			return nil, spf.ShellCompDirectiveFilterDirs
		}
		// The one kind left is [spec.FuncCompletion]. The command's context is the one Run was
		// given, which cobra sets on every command it executes.
		values, err := fn(cmd.Context(), spec.CompletionInput{ToComplete: toComplete, Args: slices.Clone(args)})
		if err != nil {
			return nil, spf.ShellCompDirectiveNoFileComp
		}
		return values, spf.ShellCompDirectiveNoFileComp
	}
}

// unknownCommand rejects any positional argument of a command that has subcommands: cobra
// leaves an argument that names no subcommand among the positional arguments, so the first one
// is a misspelt or missing subcommand.
func unknownCommand(cmd *spf.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return &UsageError{
		Path: relPath(cmd),
		Err:  fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath()),
	}
}

// hasShorthand reports whether one of flags has the shorthand s.
func hasShorthand(flags []spec.Flag, s string) bool {
	for _, f := range flags {
		if f.Shorthand() == s {
			return true
		}
	}
	return false
}

// define adds f to fs with its default, by its kind.
func define(fs *pflag.FlagSet, f spec.Flag) error {
	switch f.Kind() {
	case spec.BoolKind:
		def, _ := f.DefaultValue().(bool)
		fs.BoolP(f.Name(), f.Shorthand(), def, f.Usage())
	case spec.StringKind:
		def, _ := f.DefaultValue().(string)
		fs.StringP(f.Name(), f.Shorthand(), def, f.Usage())
	case spec.IntKind:
		def, _ := f.DefaultValue().(int)
		fs.IntP(f.Name(), f.Shorthand(), def, f.Usage())
	default:
		return fmt.Errorf("flag --%s has unsupported kind %s", f.Name(), f.Kind())
	}
	return nil
}

// read returns the parsed value of every flag in flags, keyed by name, and the names of those
// the command line sets explicitly, as pflag reports them changed: a flag given its default
// value, such as --json=false, is set. fs is the flag set of the command that runs, which holds
// its own flags and those inherited from its ancestors once cobra has parsed the command line.
func read(fs *pflag.FlagSet, flags []spec.Flag) (map[string]any, []string, error) {
	values := make(map[string]any, len(flags))
	var set []string
	for _, f := range flags {
		var (
			v   any
			err error
		)
		switch f.Kind() {
		case spec.BoolKind:
			v, err = fs.GetBool(f.Name())
		case spec.StringKind:
			v, err = fs.GetString(f.Name())
		case spec.IntKind:
			v, err = fs.GetInt(f.Name())
		default:
			err = fmt.Errorf("flag --%s has unsupported kind %s", f.Name(), f.Kind())
		}
		if err != nil {
			return nil, nil, err
		}
		values[f.Name()] = v
		if fs.Changed(f.Name()) {
			set = append(set, f.Name())
		}
	}
	return values, set, nil
}

// relPath returns the command path of cmd without the root's name.
func relPath(cmd *spf.Command) string {
	_, rest, _ := strings.Cut(cmd.CommandPath(), " ")
	return rest
}

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

// Run executes root with args against env and returns the error of the command line or of the
// run function it selects. root must be valid as [spec.Command.Validate] reports it, and env
// resolved as [spec.Env.Resolved] returns it. A nil args is no arguments; it never falls back
// to the arguments of the process.
//
// The tree answers --help, on every command, by writing the command's help to env.Stdout and
// returning nil. It has no help or completion subcommand, and refuses a tree in which a command
// declares a flag named help. A command with subcommands and no Run writes its help to
// env.Stdout and returns nil when it is given no positional argument, and returns a
// [*UsageError] naming the first one when it is.
func Run(ctx context.Context, env *spec.Env, root *spec.Command, args []string) error {
	cmd, err := build(env, root, nil)
	if err != nil {
		return err
	}
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommand(&spf.Command{Hidden: true})
	cmd.SetFlagErrorFunc(func(c *spf.Command, err error) error {
		return &UsageError{Path: relPath(c), Err: err}
	})
	cmd.SetIn(env.Stdin)
	cmd.SetOut(env.Stdout)
	cmd.SetErr(env.Stderr)
	if args == nil {
		args = []string{}
	}
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

// build returns the cobra command for c and, recursively, its subcommands. inherited are the
// inherited flags of c's ancestors, which c reads along with its own.
func build(env *spec.Env, c *spec.Command, inherited []spec.Flag) (*spf.Command, error) {
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
		visible = append(visible, f)
	}

	// Cobra's own --help flag takes the shorthand -h unconditionally, and pflag panics when a
	// flag of the command already holds it, so the help flag is declared here and gives up -h.
	if hasShorthand(visible, "h") {
		cmd.Flags().Bool("help", false, "help for "+c.Name)
	} else {
		cmd.Flags().BoolP("help", "h", false, "help for "+c.Name)
	}

	for _, sub := range c.Commands {
		child, err := build(env, sub, children)
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

	policy, hasCommands, run := c.Args, len(c.Commands) > 0, c.Run
	cmd.Args = func(cmd *spf.Command, args []string) error {
		err := policy.Check(args)
		if err == nil {
			return nil
		}
		if limit, bounded := policy.Max(); hasCommands && bounded && len(args) > limit {
			return unknownCommand(cmd, args)
		}
		return &UsageError{Path: relPath(cmd), Err: err}
	}
	cmd.RunE = func(cmd *spf.Command, args []string) error {
		values, err := read(cmd.Flags(), visible)
		if err != nil {
			return err
		}
		return run(cmd.Context(), spec.NewInvocation(env, args, values))
	}
	return cmd, nil
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

// read returns the parsed value of every flag in flags, keyed by name. fs is the flag set of the
// command that runs, which holds its own flags and those inherited from its ancestors once cobra
// has parsed the command line.
func read(fs *pflag.FlagSet, flags []spec.Flag) (map[string]any, error) {
	values := make(map[string]any, len(flags))
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
			return nil, err
		}
		values[f.Name()] = v
	}
	return values, nil
}

// relPath returns the command path of cmd without the root's name.
func relPath(cmd *spf.Command) string {
	_, rest, _ := strings.Cut(cmd.CommandPath(), " ")
	return rest
}

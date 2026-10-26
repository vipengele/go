package spec

import (
	"context"
	"errors"
	"fmt"

	"go.vipengele.org/commons/validate"
)

// Command describes one command of a tool and, through Commands, the tree below it. It is
// immutable data: it holds no state of an execution, so one tree may be run any number of
// times, concurrently. Everything an execution produces lives in its [Invocation].
type Command struct {
	// Name is the word that selects the command: a lowercase slug as [validate.Slug] accepts.
	Name string
	// Summary is the one-line description shown in the parent's list of commands.
	Summary string
	// Description is the full description shown in the command's own help.
	Description string
	// Args is the number of positional arguments the command accepts. The zero value is
	// [NoArgs].
	Args ArgPolicy
	// ArgCompletion is how a shell completes the command's positional arguments. The zero value
	// is [NoCompletion]; a command whose Args accepts no argument takes none.
	ArgCompletion Completion
	// Flags are the flags the command declares, created by [BoolFlag], [StringFlag] and
	// [IntFlag]. An inherited flag is also available to every descendant.
	Flags []Flag
	// Run executes the command. It may be nil only for a command that has subcommands.
	Run func(ctx context.Context, inv *Invocation) error
	// Commands are the subcommands.
	Commands []*Command
	// Hidden leaves the command out of its parent's help, while it still runs.
	Hidden bool
	// Annotations are key-value metadata about the command, read by the framework and by
	// hooks through [CommandInfo]; [SkipUpdateCheck] is one such key. An annotation applies to
	// the command that carries it alone, not to its subcommands.
	Annotations map[string]string
}

// SkipUpdateCheck is the annotation key that exempts a command from the update check that
// otherwise runs around it. Any value, the empty string included, exempts it.
const SkipUpdateCheck = "vipengele.skip-update-check"

// SkipConfig is the annotation key that runs a command without configuration loaded. Any value,
// the empty string included, exempts it. An annotation applies to the command that carries it
// alone, not to its subcommands.
const SkipConfig = "vipengele.skip-config"

// CommandError reports a problem with one command of a tree.
type CommandError struct {
	// Path is the space-separated names from the root to the command, as in "tool remote add".
	Path string
	// Err is the problem. It wraps [validate.ErrEmpty] for an empty name and
	// [validate.ErrInvalid] for every other problem.
	Err error
}

// Error names the command by its path, followed by the problem.
func (e *CommandError) Error() string {
	return fmt.Sprintf("command %q: %v", e.Path, e.Err)
}

// Unwrap returns the problem, so errors.Is and errors.As see through a CommandError.
func (e *CommandError) Unwrap() error {
	return e.Err
}

// Validate checks c and every command below it, and returns nil when the whole tree is valid.
// Otherwise it returns one error joining a [*CommandError] for each problem, so a caller sees
// every problem at once. It rejects:
//
//   - a nil command, and a nil entry in Commands or Flags;
//   - a Name that is not a lowercase slug, as [validate.Slug] accepts;
//   - two subcommands of one command with the same name;
//   - a command with neither Run nor Commands;
//   - an invalid Args, as [ArgPolicy.Validate] reports it;
//   - an ArgCompletion on a command whose Args accepts no argument, and an ArgCompletion built
//     by [CompleteFunc] with a nil function;
//   - a flag with an invalid name, an invalid shorthand, or a default of another type;
//   - a bool flag given a completion by [Complete], and a flag completion built by
//     [CompleteFunc] with a nil function;
//   - two flags with the same name, or the same shorthand, visible to one command: declared by
//     it, or inherited from one of its ancestors.
func (c *Command) Validate() error {
	if c == nil {
		return &CommandError{Err: fmt.Errorf("%w: command is nil", validate.ErrInvalid)}
	}
	var errs []error
	c.validate("", flagScope{}, &errs)
	return errors.Join(errs...)
}

// flagScope maps the names and shorthands of the inherited flags of a command's ancestors to
// the path of the command declaring each.
type flagScope struct {
	names  map[string]string
	shorts map[string]string
}

func (s flagScope) with(f Flag, path string) flagScope {
	next := flagScope{names: map[string]string{}, shorts: map[string]string{}}
	for k, v := range s.names {
		next.names[k] = v
	}
	for k, v := range s.shorts {
		next.shorts[k] = v
	}
	next.names[f.Name()] = path
	if f.Shorthand() != "" {
		next.shorts[f.Shorthand()] = path
	}
	return next
}

func (c *Command) validate(parent string, inherited flagScope, errs *[]error) {
	path := c.Name
	if parent != "" {
		path = parent + " " + c.Name
	}
	report := func(err error) {
		*errs = append(*errs, &CommandError{Path: path, Err: err})
	}

	if err := validate.Slug("Name", c.Name); err != nil {
		report(err)
	}
	if c.Run == nil && len(c.Commands) == 0 {
		report(fmt.Errorf("%w: has neither Run nor Commands", validate.ErrInvalid))
	}
	if err := c.Args.Validate(); err != nil {
		report(err)
	}
	if c.ArgCompletion.Kind() != NoCompletion {
		if limit, bounded := c.Args.Max(); bounded && limit == 0 {
			report(fmt.Errorf("%w: ArgCompletion on a command that accepts no arguments", validate.ErrInvalid))
		}
	}
	if err := c.ArgCompletion.problem(); err != nil {
		report(fmt.Errorf("ArgCompletion: %w", err))
	}

	children := inherited
	names := map[string]bool{}
	shorts := map[string]bool{}
	for i, f := range c.Flags {
		if f == nil {
			report(fmt.Errorf("%w: Flags[%d] is nil", validate.ErrInvalid, i))
			continue
		}
		for _, err := range f.problems() {
			report(err)
		}
		name, short := f.Name(), f.Shorthand()
		if owner, ok := inherited.names[name]; ok {
			report(fmt.Errorf("%w: flag --%s collides with the flag inherited from command %q",
				validate.ErrInvalid, name, owner))
		} else if names[name] {
			report(fmt.Errorf("%w: flag --%s is declared twice", validate.ErrInvalid, name))
		}
		if short != "" {
			if owner, ok := inherited.shorts[short]; ok {
				report(fmt.Errorf("%w: shorthand -%s of flag --%s collides with the flag inherited from command %q",
					validate.ErrInvalid, short, name, owner))
			} else if shorts[short] {
				report(fmt.Errorf("%w: shorthand -%s is declared twice", validate.ErrInvalid, short))
			}
		}
		names[name] = true
		if short != "" {
			shorts[short] = true
		}
		if f.Inherited() {
			children = children.with(f, path)
		}
	}

	seen := map[string]bool{}
	for i, sub := range c.Commands {
		if sub == nil {
			report(fmt.Errorf("%w: Commands[%d] is nil", validate.ErrInvalid, i))
			continue
		}
		if seen[sub.Name] {
			report(fmt.Errorf("%w: subcommand %q is declared twice", validate.ErrInvalid, sub.Name))
		}
		seen[sub.Name] = true
		sub.validate(path, children, errs)
	}
}

package spec

import (
	"fmt"

	"go.vipengele.org/commons/validate"
)

// FlagGroupKind is the constraint a [FlagGroup] places on its flags.
type FlagGroupKind int

// The kinds a [FlagGroup] has. The zero FlagGroupKind is no kind, so a FlagGroup whose Kind is
// left unset fails [Command.Validate] rather than silently taking one of these constraints.
const (
	// MutuallyExclusive allows at most one of the group's flags on a command line.
	MutuallyExclusive FlagGroupKind = iota + 1
	// RequiredTogether requires every flag of the group on a command line that sets any of them.
	RequiredTogether
)

// String returns "mutually exclusive" or "required together", and "FlagGroupKind(n)" for any
// other value.
func (k FlagGroupKind) String() string {
	switch k {
	case MutuallyExclusive:
		return "mutually exclusive"
	case RequiredTogether:
		return "required together"
	}
	return fmt.Sprintf("FlagGroupKind(%d)", int(k))
}

// FlagGroup constrains which flags of one command line may, or must, be set together. A flag
// counts as set when the command line gives it explicitly, as [FlagOf.IsSet] reports it, so a
// default never counts. The constraint applies to the command that lists the group alone, not
// to its subcommands, and only a command with a Run may list one: a command without Run never
// runs, so its groups would never apply. The adapter that runs the command enforces it.
type FlagGroup struct {
	// Kind is the constraint: [MutuallyExclusive] or [RequiredTogether].
	Kind FlagGroupKind
	// Flags are the names of the flags in the group, without dashes, as [Flag.Name] returns them.
	// Each is visible to the command: declared by it, or inherited from one of its ancestors.
	Flags []string
}

// validateFlagGroups reports the problems of c's flag groups. visible holds the names of the
// flags c declares, and inherited the names of the flags its ancestors pass down to it.
func (c *Command) validateFlagGroups(inherited flagScope, visible map[string]bool, report func(error)) {
	if len(c.FlagGroups) > 0 && c.Run == nil {
		report(fmt.Errorf("%w: FlagGroups on a command with no Run, which never runs to apply them", validate.ErrInvalid))
	}
	for i, g := range c.FlagGroups {
		where := fmt.Sprintf("FlagGroups[%d]", i)
		if g.Kind != MutuallyExclusive && g.Kind != RequiredTogether {
			report(fmt.Errorf("%w: %s has unknown kind %v", validate.ErrInvalid, where, g.Kind))
		}
		if len(g.Flags) < 2 {
			report(fmt.Errorf("%w: %s names %d flags, want at least 2", validate.ErrInvalid, where, len(g.Flags)))
		}
		seen := map[string]bool{}
		for _, name := range g.Flags {
			if seen[name] {
				report(fmt.Errorf("%w: %s names flag --%s twice", validate.ErrInvalid, where, name))
				continue
			}
			seen[name] = true
			if _, ok := inherited.names[name]; !ok && !visible[name] {
				report(fmt.Errorf("%w: %s names flag --%s, which the command neither declares nor inherits",
					validate.ErrInvalid, where, name))
			}
		}
	}
}

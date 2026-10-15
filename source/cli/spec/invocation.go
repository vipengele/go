package spec

import (
	"maps"
	"slices"
)

// Invocation is what a run function receives for one execution of a command: its environment,
// its positional arguments, its output settings, and the flag values, which are read through
// the flags' handles with [FlagOf.Get].
type Invocation struct {
	// Env is the environment of the execution. [NewInvocation] never leaves it nil, nor any of
	// its reader, writer or lookup fields.
	Env *Env
	// Args are the positional arguments, flags removed.
	Args []string
	// Output is how the execution presents its output, resolved once per run by the adapter.
	// [NewInvocation] leaves it at the zero value, which is the default presentation.
	Output Output

	values map[string]any
}

// NewInvocation returns the invocation an adapter passes to a run function. env is resolved
// with [Env.Resolved]. values maps a flag's name to its value, a bool, string or int as the
// flag's kind says; a flag absent from values reads as its default. args and values are copied,
// so the caller may reuse them.
func NewInvocation(env *Env, args []string, values map[string]any) *Invocation {
	return &Invocation{
		Env:    env.Resolved(),
		Args:   slices.Clone(args),
		values: maps.Clone(values),
	}
}

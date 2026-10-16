package spec

import (
	"maps"
	"slices"
)

// Invocation is what a run function receives for one execution of a command: its environment,
// its positional arguments, its output settings, and the flag values, which are read through
// the flags' handles with [FlagOf.Get]. Whether the command line sets a flag explicitly, rather
// than leaving it at its default, is read with [FlagOf.IsSet].
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
	set    map[string]bool
}

// InvocationOption configures the [Invocation] [NewInvocation] builds.
type InvocationOption func(*Invocation)

// ExplicitFlags names the flags the command line sets explicitly, by their long names, so that
// [FlagOf.IsSet] tells a flag given its default value, such as --json=false, from a flag not
// given at all. Without it no flag reads as set. An adapter passes the names its framework
// reports as changed by the command line.
func ExplicitFlags(names ...string) InvocationOption {
	return func(inv *Invocation) {
		if inv.set == nil {
			inv.set = make(map[string]bool, len(names))
		}
		for _, name := range names {
			inv.set[name] = true
		}
	}
}

// NewInvocation returns the invocation an adapter passes to a run function, configured by opts
// in order. env is resolved with [Env.Resolved]. values maps a flag's name to its value, a bool,
// string or int as the flag's kind says; a flag absent from values reads as its default. args
// and values are copied, so the caller may reuse them.
func NewInvocation(env *Env, args []string, values map[string]any, opts ...InvocationOption) *Invocation {
	inv := &Invocation{
		Env:    env.Resolved(),
		Args:   slices.Clone(args),
		values: maps.Clone(values),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(inv)
		}
	}
	return inv
}

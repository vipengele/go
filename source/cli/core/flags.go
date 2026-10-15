package cli

import "go.vipengele.org/cli/spec"

// WithVerbose gives the App the standard flag -v/--verbose, which asks for more detail than the
// default: [spec.Output.Verbose] reports it. Like every standard flag it is declared on the root
// and inherited by every command, so a command that declares a flag of the same name or
// shorthand makes the tree invalid. Given with --quiet, it is a usage error.
func WithVerbose() Option {
	return func(s *settings) { s.verbose = true }
}

// WithQuiet gives the App the standard flag -q/--quiet, which asks for less output than the
// default: [spec.Output.Quiet] reports it. It is inherited as [WithVerbose] describes, and given
// with --verbose, it is a usage error.
func WithQuiet() Option {
	return func(s *settings) { s.quiet = true }
}

// WithNoColor gives the App the standard flag --no-color, which forbids colour:
// [spec.Output.Color] reports false. It is inherited as [WithVerbose] describes. Colour is also
// forbidden, with or without the flag, by the environment variables NO_COLOR and
// <EnvPrefix>_NO_COLOR, and when the standard output is not a terminal.
func WithNoColor() Option {
	return func(s *settings) { s.noColor = true }
}

// WithJSON gives the App the standard flag --json, which asks for machine-readable JSON instead
// of text: [spec.Output.JSON] reports it. It is inherited as [WithVerbose] describes, and has no
// bearing on colour.
func WithJSON() Option {
	return func(s *settings) { s.json = true }
}

// standardFlags holds the handles of the standard flags of one execution. A nil handle is a
// flag the App does not take. The handles are built per execution, so no two executions share
// one.
type standardFlags struct {
	verbose *spec.FlagOf[bool]
	quiet   *spec.FlagOf[bool]
	noColor *spec.FlagOf[bool]
	json    *spec.FlagOf[bool]
}

// newStandardFlags returns the handles of the standard flags s opts into.
func newStandardFlags(s settings) standardFlags {
	var f standardFlags
	if s.verbose {
		f.verbose = spec.BoolFlag("verbose", "print more detail", spec.Shorthand("v"), spec.Inherited())
	}
	if s.quiet {
		f.quiet = spec.BoolFlag("quiet", "print less output", spec.Shorthand("q"), spec.Inherited())
	}
	if s.noColor {
		f.noColor = spec.BoolFlag("no-color", "do not use colour", spec.Inherited())
	}
	if s.json {
		f.json = spec.BoolFlag("json", "write JSON instead of text", spec.Inherited())
	}
	return f
}

// list returns the flags f holds, in the order -v, -q, --no-color, --json.
func (f standardFlags) list() []spec.Flag {
	var flags []spec.Flag
	for _, h := range []*spec.FlagOf[bool]{f.verbose, f.quiet, f.noColor, f.json} {
		if h != nil {
			flags = append(flags, h)
		}
	}
	return flags
}

// isSet returns the value of h in inv, and false for a nil h.
func isSet(h *spec.FlagOf[bool], inv *spec.Invocation) bool {
	return h != nil && h.Get(inv)
}

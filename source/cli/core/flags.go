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

// WithNoColor gives the App the standard flag --no-color, which forbids colour on every stream:
// [spec.Output.Color] reports false for [spec.Stdout] and [spec.Stderr] alike. It is inherited as
// [WithVerbose] describes. Without the flag, colour is decided for each stream separately by
// [spec.ColorAllowed], from NO_COLOR, <EnvPrefix>_NO_COLOR, FORCE_COLOR, TERM and whether the Env
// reports that stream as a terminal.
func WithNoColor() Option {
	return func(s *settings) { s.noColor = true }
}

// WithJSON gives the App the standard flag --json, which asks for machine-readable JSON instead
// of text: [spec.Output.JSON] reports it, and [spec.Output.Format] reports [spec.FormatJSON]. It
// is inherited as [WithVerbose] describes, and has no bearing on colour. It is the shorthand of
// [WithOutput] for a tool whose only other format is text; an App takes one of the two, and [New]
// panics when it is given both.
func WithJSON() Option {
	return func(s *settings) { s.json = true }
}

// WithOutput gives the App the standard flag --output, which names the format of the output,
// text or json: [spec.Output.Format] reports it, and is [spec.FormatText] when the command line
// does not set --output. It is inherited as [WithVerbose] describes, and has no bearing on colour.
// Any other value is a usage error.
//
// A command that declares a --json of its own, such as "version", keeps it: the command reads its
// own --json together with [spec.Output.Format], and --output text with a --json that is true, or
// --output json with a --json explicitly false, is a usage error. An App takes one of WithOutput
// and [WithJSON], and [New] panics when it is given both.
func WithOutput() Option {
	return func(s *settings) { s.output = true }
}

// standardFlags holds the handles of the standard flags of one execution. A nil handle is a
// flag the App does not take. The handles are built per execution, so no two executions share
// one.
type standardFlags struct {
	verbose *spec.FlagOf[bool]
	quiet   *spec.FlagOf[bool]
	noColor *spec.FlagOf[bool]
	json    *spec.FlagOf[bool]
	output  *spec.FlagOf[string]
	// localJSON reads the --json the running command declares itself, which --output must agree
	// with. It is set exactly when output is, and the root does not declare it.
	localJSON *spec.FlagOf[bool]
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
	if s.output {
		f.output = spec.StringFlag("output", "write the output as text or json",
			spec.Default(spec.FormatText.String()), spec.Inherited())
		f.localJSON = spec.BoolFlag("json", "write JSON instead of text")
	}
	return f
}

// list returns the flags the root declares, in the order -v, -q, --no-color, --json, --output.
func (f standardFlags) list() []spec.Flag {
	var flags []spec.Flag
	for _, h := range []*spec.FlagOf[bool]{f.verbose, f.quiet, f.noColor, f.json} {
		if h != nil {
			flags = append(flags, h)
		}
	}
	if f.output != nil {
		flags = append(flags, f.output)
	}
	return flags
}

// isSet returns the value of h in inv, and false for a nil h.
func isSet(h *spec.FlagOf[bool], inv *spec.Invocation) bool {
	return h != nil && h.Get(inv)
}

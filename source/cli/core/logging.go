package cli

import (
	"errors"

	"go.vipengele.org/cli/spec"
)

// WithLogging gives the App the flags --log-level and --log-format, which configure the
// process-wide logger the go.vipengele.org/cli/core/logging package builds. Both are declared on
// the root and inherited by every command, as [WithVerbose] describes, after the standard flags.
//
// --log-level names the minimum level of a record: debug, info, warn or error. --log-format names
// the format of the records, text or json, the spellings of [WithOutput], and is text when the
// command line does not set it.
//
// The App also needs [WithVerbose] and [WithQuiet]: without either, every execution returns
// [ExitError] after writing "<name>: invalid command tree" to env.Stderr, followed by a row naming
// the options it lacks, and calls no run function.
func WithLogging() Option {
	return func(s *settings) { s.logging = true }
}

// loggingFlags holds the handles of the flags [WithLogging] declares for one execution. Both
// handles are nil when the App does not take the option.
type loggingFlags struct {
	// level declares no default: the level a command line that does not set --log-level runs at
	// follows -v and -q, so a declared default would show in --help as a level that does not
	// apply, and "" reads unambiguously as not given beside [spec.FlagOf.IsSet].
	level  *spec.FlagOf[string]
	format *spec.FlagOf[string]
}

// newLoggingFlags returns the handles of the flags [WithLogging] declares, or none when s does
// not opt into it.
func newLoggingFlags(s settings) loggingFlags {
	if !s.logging {
		return loggingFlags{}
	}
	return loggingFlags{
		level: spec.StringFlag("log-level", "write log records at or above debug, info, warn or error",
			spec.Inherited()),
		format: spec.StringFlag("log-format", "write log records as text or json",
			spec.Default(spec.FormatText.String()), spec.Inherited()),
	}
}

// list returns the flags the root declares for [WithLogging], in the order --log-level,
// --log-format.
func (f loggingFlags) list() []spec.Flag {
	if f.level == nil {
		return nil
	}
	return []spec.Flag{f.level, f.format}
}

// loggingProblem returns the problem of options s that hold [WithLogging] without both
// [WithVerbose] and [WithQuiet], and nil for any other options.
func loggingProblem(s settings) error {
	if !s.logging || (s.verbose && s.quiet) {
		return nil
	}
	return errors.New("WithLogging requires WithVerbose and WithQuiet")
}

package cli

import (
	"errors"

	"go.vipengele.org/cli/spec"
)

// outputResolver resolves the [spec.Output] of one execution from its standard flags and its
// environment. The zero value takes no standard flag and reads no prefixed variable.
type outputResolver struct {
	// identity names <EnvPrefix>_NO_COLOR. Nil reads no prefixed variable.
	identity *spec.Identity
	// flags are the standard flags of the execution.
	flags standardFlags
}

// resolve returns the Output of inv. It returns an error, which the adapter reports as a usage
// error, when inv sets both --verbose and --quiet. Colour is decided for [spec.Stdout] and
// [spec.Stderr] separately, each by [spec.ColorAllowed] with --no-color, the identity and
// inv.Env, which must be resolved as [spec.Env.Resolved] returns it.
func (r outputResolver) resolve(inv *spec.Invocation) (spec.Output, error) {
	verbose, quiet := isSet(r.flags.verbose, inv), isSet(r.flags.quiet, inv)
	if verbose && quiet {
		return spec.Output{}, errors.New("--verbose and --quiet cannot be used together")
	}
	noColor := isSet(r.flags.noColor, inv)
	return spec.NewOutput(spec.OutputOptions{
		Verbose:       verbose,
		Quiet:         quiet,
		JSON:          isSet(r.flags.json, inv),
		NoColorStdout: !spec.ColorAllowed(noColor, r.identity, inv.Env, spec.Stdout),
		NoColorStderr: !spec.ColorAllowed(noColor, r.identity, inv.Env, spec.Stderr),
	}), nil
}

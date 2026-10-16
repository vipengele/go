package cli

import (
	"errors"
	"fmt"

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
// error, when inv sets both --verbose and --quiet, or a format [outputResolver.format] refuses.
// Colour is decided for [spec.Stdout] and [spec.Stderr] separately, each by [spec.ColorAllowed]
// with --no-color, the identity and inv.Env, which must be resolved as [spec.Env.Resolved]
// returns it.
func (r outputResolver) resolve(inv *spec.Invocation) (spec.Output, error) {
	verbose, quiet := isSet(r.flags.verbose, inv), isSet(r.flags.quiet, inv)
	if verbose && quiet {
		return spec.Output{}, errors.New("--verbose and --quiet cannot be used together")
	}
	format, err := r.format(inv)
	if err != nil {
		return spec.Output{}, err
	}
	noColor := isSet(r.flags.noColor, inv)
	return spec.NewOutput(spec.OutputOptions{
		Verbose:       verbose,
		Quiet:         quiet,
		Format:        format,
		NoColorStdout: !spec.ColorAllowed(noColor, r.identity, inv.Env, spec.Stdout),
		NoColorStderr: !spec.ColorAllowed(noColor, r.identity, inv.Env, spec.Stderr),
	}), nil
}

// format returns the format of inv. The standard --json selects JSON. Under --output, a command
// line that does not set it is text, whatever the running command's own --json, which that
// command reads itself; an --output it sets is the format, and it must agree with that --json:
// --output text with a --json that is true, --output json with a --json set explicitly to false,
// and a value other than text or json are errors.
func (r outputResolver) format(inv *spec.Invocation) (spec.Format, error) {
	if r.flags.output == nil {
		if isSet(r.flags.json, inv) {
			return spec.FormatJSON, nil
		}
		return spec.FormatText, nil
	}
	if !r.flags.output.IsSet(inv) {
		return spec.FormatText, nil
	}
	asJSON := isSet(r.flags.localJSON, inv)
	switch value := r.flags.output.Get(inv); value {
	case spec.FormatText.String():
		if asJSON {
			return spec.FormatText, errors.New("--output text and --json cannot be used together")
		}
		return spec.FormatText, nil
	case spec.FormatJSON.String():
		if !asJSON && r.flags.localJSON.IsSet(inv) {
			return spec.FormatJSON, errors.New("--output json and --json=false cannot be used together")
		}
		return spec.FormatJSON, nil
	default:
		return spec.FormatText, fmt.Errorf("--output must be %s or %s, not %q",
			spec.FormatText, spec.FormatJSON, value)
	}
}

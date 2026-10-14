package cli

import (
	"errors"
	"io"
	"os"

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
// error, when inv sets both --verbose and --quiet.
func (r outputResolver) resolve(inv *spec.Invocation) (spec.Output, error) {
	verbose, quiet := isSet(r.flags.verbose, inv), isSet(r.flags.quiet, inv)
	if verbose && quiet {
		return spec.Output{}, errors.New("--verbose and --quiet cannot be used together")
	}
	return spec.NewOutput(spec.OutputOptions{
		Verbose: verbose,
		Quiet:   quiet,
		JSON:    isSet(r.flags.json, inv),
		NoColor: isSet(r.flags.noColor, inv) || r.colorForbidden(inv.Env),
	}), nil
}

// colorForbidden reports whether env forbids colour: NO_COLOR or <EnvPrefix>_NO_COLOR is set and
// not empty, or env.Stdout is not a terminal. env must be resolved as [spec.Env.Resolved]
// returns it. An identity whose EnvPrefix names no variable reads no prefixed variable.
func (r outputResolver) colorForbidden(env *spec.Env) bool {
	if nonEmpty(env, "NO_COLOR") {
		return true
	}
	if r.identity != nil {
		if name, err := r.identity.EnvVar("NO_COLOR"); err == nil && nonEmpty(env, name) {
			return true
		}
	}
	return !isTerminal(env.Stdout)
}

// nonEmpty reports whether env sets the variable name to a value that is not empty.
func nonEmpty(env *spec.Env, name string) bool {
	v, ok := env.LookupEnv(name)
	return ok && v != ""
}

// isTerminal reports whether w is an *os.File open on a character device. Any other writer,
// and a file whose mode cannot be read, is not a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

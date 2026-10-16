package spec

// Output is how one execution presents its output: how much it says, whether it writes JSON,
// and whether it may use colour on each output stream. An adapter resolves it once per run, from
// the standard flags and the environment, and sets it on the [Invocation]; a run function reads
// it through the accessors. Colour is decided for [Stdout] and [Stderr] separately, so a run
// whose standard output is redirected keeps colour on a standard error that is a terminal. The
// zero value is the default presentation: neither verbose nor quiet, text rather than JSON, and
// colour allowed on both output streams.
type Output struct {
	verbose       bool
	quiet         bool
	json          bool
	noColorStdout bool
	noColorStderr bool
}

// OutputOptions are the settings [NewOutput] builds an [Output] from. The zero value builds the
// zero Output.
type OutputOptions struct {
	// Verbose asks for more detail than the default.
	Verbose bool
	// Quiet asks for less output than the default.
	Quiet bool
	// JSON asks for machine-readable JSON instead of text.
	JSON bool
	// NoColorStdout forbids colour on [Stdout].
	NoColorStdout bool
	// NoColorStderr forbids colour on [Stderr].
	NoColorStderr bool
}

// NewOutput returns the Output opts describe. It keeps every setting as given; deciding between
// settings that conflict, such as Verbose and Quiet together, is the caller's.
func NewOutput(opts OutputOptions) Output {
	return Output{
		verbose:       opts.Verbose,
		quiet:         opts.Quiet,
		json:          opts.JSON,
		noColorStdout: opts.NoColorStdout,
		noColorStderr: opts.NoColorStderr,
	}
}

// Verbose reports whether the execution asks for more detail than the default.
func (o Output) Verbose() bool { return o.verbose }

// Quiet reports whether the execution asks for less output than the default.
func (o Output) Quiet() bool { return o.quiet }

// JSON reports whether the execution writes machine-readable JSON instead of text.
func (o Output) JSON() bool { return o.json }

// Color reports whether the execution may use colour on stream s. It is true for [Stdout] and
// [Stderr] unless colour is forbidden on that stream, and false for [Stdin] and any other
// stream, which take no output.
func (o Output) Color(s Stream) bool {
	switch s {
	case Stdout:
		return !o.noColorStdout
	case Stderr:
		return !o.noColorStderr
	default:
		return false
	}
}

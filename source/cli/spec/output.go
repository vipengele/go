package spec

// Output is how one execution presents its output: how much it says, whether it writes JSON,
// and whether it may use colour. An adapter resolves it once per run, from the standard flags
// and the environment, and sets it on the [Invocation]; a run function reads it through the
// accessors. The zero value is the default presentation: neither verbose nor quiet, text rather
// than JSON, and colour allowed.
type Output struct {
	verbose bool
	quiet   bool
	json    bool
	noColor bool
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
	// NoColor forbids colour.
	NoColor bool
}

// NewOutput returns the Output opts describe. It keeps every setting as given; deciding between
// settings that conflict, such as Verbose and Quiet together, is the caller's.
func NewOutput(opts OutputOptions) Output {
	return Output{
		verbose: opts.Verbose,
		quiet:   opts.Quiet,
		json:    opts.JSON,
		noColor: opts.NoColor,
	}
}

// Verbose reports whether the execution asks for more detail than the default.
func (o Output) Verbose() bool { return o.verbose }

// Quiet reports whether the execution asks for less output than the default.
func (o Output) Quiet() bool { return o.quiet }

// JSON reports whether the execution writes machine-readable JSON instead of text.
func (o Output) JSON() bool { return o.json }

// Color reports whether the execution may use colour. It is true unless colour is forbidden;
// whether a given stream supports colour is for the writer to decide.
func (o Output) Color() bool { return !o.noColor }

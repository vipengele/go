package spec

// Format is the form an execution writes its output in. The zero value is [FormatText].
type Format int

const (
	// FormatText is human-readable text.
	FormatText Format = iota
	// FormatJSON is machine-readable JSON.
	FormatJSON
)

// String returns the name of f, "text" or "json"; any other value is "unknown".
func (f Format) String() string {
	switch f {
	case FormatText:
		return "text"
	case FormatJSON:
		return "json"
	default:
		return "unknown"
	}
}

// Output is how one execution presents its output: how much it says, which [Format] it writes,
// and whether it may use colour on each output stream. An adapter resolves it once per run, from
// the standard flags and the environment, and sets it on the [Invocation]; a run function reads
// it through the accessors. Colour is decided for [Stdout] and [Stderr] separately, so a run
// whose standard output is redirected keeps colour on a standard error that is a terminal. The
// zero value is the default presentation: neither verbose nor quiet, text rather than JSON, and
// colour allowed on both output streams.
type Output struct {
	verbose       bool
	quiet         bool
	format        Format
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
	// Format is the form the output is written in.
	Format Format
	// JSON asks for machine-readable JSON instead of text. It is the same as Format being
	// [FormatJSON]: either one selects JSON.
	JSON bool
	// NoColorStdout forbids colour on [Stdout].
	NoColorStdout bool
	// NoColorStderr forbids colour on [Stderr].
	NoColorStderr bool
}

// NewOutput returns the Output opts describe. It keeps every setting as given; deciding between
// settings that conflict, such as Verbose and Quiet together, is the caller's.
func NewOutput(opts OutputOptions) Output {
	format := opts.Format
	if opts.JSON {
		format = FormatJSON
	}
	return Output{
		verbose:       opts.Verbose,
		quiet:         opts.Quiet,
		format:        format,
		noColorStdout: opts.NoColorStdout,
		noColorStderr: opts.NoColorStderr,
	}
}

// Verbose reports whether the execution asks for more detail than the default.
func (o Output) Verbose() bool { return o.verbose }

// Quiet reports whether the execution asks for less output than the default.
func (o Output) Quiet() bool { return o.quiet }

// Format returns the form the execution writes its output in.
func (o Output) Format() Format { return o.format }

// JSON reports whether the execution writes machine-readable JSON instead of text. It is true
// exactly when [Output.Format] is [FormatJSON].
func (o Output) JSON() bool { return o.format == FormatJSON }

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

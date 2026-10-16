package spec

// The glyphs that mark a message by its role. They are plain UTF-8 and the same with colour on
// or off: TERM=dumb and the other colour rules change only colour, and no ASCII fallback
// replaces a glyph (ADR-0011).
const (
	// GlyphInfo marks an informational message.
	GlyphInfo = "ℹ"
	// GlyphSuccess marks an operation that succeeded.
	GlyphSuccess = "✓"
	// GlyphWarning marks a warning.
	GlyphWarning = "⚠"
	// GlyphError marks an error.
	GlyphError = "✗"
)

// The SGR parameters a Styler writes, and the sequence that resets them.
const (
	sgrReset  = "\x1b[0m"
	sgrBold   = "1"
	sgrDim    = "2"
	sgrRed    = "31"
	sgrGreen  = "32"
	sgrYellow = "33"
	sgrBlue   = "34"
)

// Styler styles text for one output stream. It writes nothing itself: every method returns the
// string to write. With colour on, each styled run is ESC[<code>m, the text and ESC[0m, so
// removing the SGR sequences yields exactly what the same call returns with colour off; with
// colour off the text is returned with no escape at all. Glyphs are the same either way.
//
// The Styler methods below are the stable surface. The message helpers, such as
// [Invocation.Info], are methods on [Invocation] built on it. The zero Styler has colour off.
type Styler struct {
	color bool
}

// Styler returns the Styler for stream s, with colour on exactly when [Output.Color] allows it
// on s. A Styler for [Stdin] or any stream other than [Stdout] and [Stderr] has colour off.
func (o Output) Styler(s Stream) Styler {
	return Styler{color: o.Color(s)}
}

// Color reports whether the Styler writes colour.
func (st Styler) Color() bool { return st.color }

// Bold returns text in bold.
func (st Styler) Bold(text string) string { return st.sgr(sgrBold, text) }

// Dim returns text dimmed.
func (st Styler) Dim(text string) string { return st.sgr(sgrDim, text) }

// Info returns text marked as information: [GlyphInfo], a space and text, in blue.
func (st Styler) Info(text string) string { return st.sgr(sgrBlue, GlyphInfo+" "+text) }

// Success returns text marked as a success: [GlyphSuccess], a space and text, in green.
func (st Styler) Success(text string) string { return st.sgr(sgrGreen, GlyphSuccess+" "+text) }

// Warning returns text marked as a warning: [GlyphWarning], a space and text, in yellow.
func (st Styler) Warning(text string) string { return st.sgr(sgrYellow, GlyphWarning+" "+text) }

// Error returns text marked as an error: [GlyphError], a space and text, in red.
func (st Styler) Error(text string) string { return st.sgr(sgrRed, GlyphError+" "+text) }

// sgr wraps text in the SGR sequence for code and a reset when colour is on. Empty text is
// returned as is, so no run of escapes surrounds nothing.
func (st Styler) sgr(code, text string) string {
	if !st.color || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + sgrReset
}

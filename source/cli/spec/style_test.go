package spec_test

import (
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

// stripSGR removes every ESC[...m sequence from s.
func stripSGR(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		j := strings.IndexByte(s[i:], 'm')
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		s = s[i+j+1:]
	}
}

// styleCalls are the Styler methods that take text, by name.
func styleCalls() []struct {
	name  string
	style func(spec.Styler, string) string
} {
	return []struct {
		name  string
		style func(spec.Styler, string) string
	}{
		{"bold", spec.Styler.Bold},
		{"dim", spec.Styler.Dim},
		{"info", spec.Styler.Info},
		{"success", spec.Styler.Success},
		{"warning", spec.Styler.Warning},
		{"error", spec.Styler.Error},
	}
}

func colourOn() spec.Styler  { return spec.Output{}.Styler(spec.Stdout) }
func colourOff() spec.Styler { return spec.Output{}.Styler(spec.Stdin) }

func TestStylerColourOnStripsToColourOff(t *testing.T) {
	for _, tc := range styleCalls() {
		t.Run(tc.name, func(t *testing.T) {
			on := tc.style(colourOn(), "hello")
			off := tc.style(colourOff(), "hello")
			if got := stripSGR(on); got != off {
				t.Errorf("stripped colour-on output = %q, want colour-off output %q", got, off)
			}
			if !strings.HasPrefix(on, "\x1b[") {
				t.Errorf("colour-on output %q does not start with an SGR sequence", on)
			}
			if !strings.HasSuffix(on, "\x1b[0m") {
				t.Errorf("colour-on output %q does not end with a reset", on)
			}
			if strings.ContainsRune(off, '\x1b') {
				t.Errorf("colour-off output %q contains an ESC byte", off)
			}
		})
	}
}

func TestStylerEmptyTextHasNoEscapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		style func(spec.Styler, string) string
	}{{"bold", spec.Styler.Bold}, {"dim", spec.Styler.Dim}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.style(colourOn(), ""); got != "" {
				t.Errorf("styled empty text = %q, want \"\"", got)
			}
		})
	}
}

func TestStylerGlyphsAreTheSameWithColourOnAndOff(t *testing.T) {
	tests := []struct {
		name  string
		style func(spec.Styler, string) string
		glyph string
	}{
		{"info", spec.Styler.Info, spec.GlyphInfo},
		{"success", spec.Styler.Success, spec.GlyphSuccess},
		{"warning", spec.Styler.Warning, spec.GlyphWarning},
		{"error", spec.Styler.Error, spec.GlyphError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.glyph + " text"
			if got := tc.style(colourOff(), "text"); got != want {
				t.Errorf("colour off = %q, want %q", got, want)
			}
			if got := stripSGR(tc.style(colourOn(), "text")); got != want {
				t.Errorf("colour on, stripped = %q, want %q", got, want)
			}
		})
	}
}

func TestStylerFollowsOutputColourPerStream(t *testing.T) {
	noStderr := spec.NewOutput(spec.OutputOptions{NoColorStderr: true})
	tests := []struct {
		name   string
		out    spec.Output
		stream spec.Stream
		color  bool
	}{
		{"default stdout is coloured", spec.Output{}, spec.Stdout, true},
		{"default stderr is coloured", spec.Output{}, spec.Stderr, true},
		{"stdin is plain", spec.Output{}, spec.Stdin, false},
		{"an unknown stream is plain", spec.Output{}, spec.Stream(7), false},
		{"stderr with colour forbidden is plain", noStderr, spec.Stderr, false},
		{"stdout keeps colour when only stderr forbids it", noStderr, spec.Stdout, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.out.Styler(tc.stream)
			if st.Color() != tc.color {
				t.Errorf("Color() = %v, want %v", st.Color(), tc.color)
			}
			got := st.Error("boom")
			if hasESC := strings.ContainsRune(got, '\x1b'); hasESC != tc.color {
				t.Errorf("Error(%q) = %q, contains ESC = %v, want %v", "boom", got, hasESC, tc.color)
			}
		})
	}
}

func TestZeroStylerIsPlain(t *testing.T) {
	var st spec.Styler
	if got := st.Bold("x"); got != "x" {
		t.Errorf("zero Styler Bold = %q, want %q", got, "x")
	}
}

func TestGlyphsAreNonEmptyAndDistinct(t *testing.T) {
	glyphs := []string{spec.GlyphInfo, spec.GlyphSuccess, spec.GlyphWarning, spec.GlyphError}
	seen := make(map[string]bool, len(glyphs))
	for _, g := range glyphs {
		if g == "" {
			t.Errorf("a glyph is empty")
		}
		if seen[g] {
			t.Errorf("glyph %q is used for two roles", g)
		}
		seen[g] = true
	}
}

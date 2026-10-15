package errrender_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/core/internal/errrender"
	"go.vipengele.org/cli/spec"
)

// fieldError is an error a test renders with a [errrender.Renderer].
type fieldError struct {
	field string
	err   error
}

func (e *fieldError) Error() string { return e.field + ": " + e.err.Error() }
func (e *fieldError) Unwrap() error { return e.err }

// quoted prints its wrapped error in the middle of its own message.
type quoted struct{ err error }

func (e *quoted) Error() string { return "reading \"" + e.err.Error() + "\" failed" }
func (e *quoted) Unwrap() error { return e.err }

// renderAs returns a Renderer that renders every *fieldError as out.
func renderAs(out string) errrender.Renderer {
	return func(err error) (string, bool) {
		_, ok := err.(*fieldError)
		return out, ok
	}
}

// renderField renders a *fieldError as its field followed by its problem on a detail row.
func renderField(err error) (string, bool) {
	fe, ok := err.(*fieldError)
	if !ok {
		return "", false
	}
	return "field " + fe.field + "\nproblem: " + fe.err.Error(), true
}

func TestRender(t *testing.T) {
	notExist := errors.New("file does not exist")
	syntax := errors.New("strconv: invalid syntax")
	field := &fieldError{field: "port", err: syntax}

	tests := []struct {
		name      string
		tool      string
		err       error
		renderers []errrender.Renderer
		want      string
	}{
		{
			name: "flat error prints on the summary line",
			tool: "tool",
			err:  errors.New("message"),
			want: "tool: message\n",
		},
		{
			name: "empty tool name leaves the summary unprefixed",
			err:  errors.New("message"),
			want: "message\n",
		},
		{
			name: "each wrap layer is an arrow row",
			tool: "tool",
			err:  fmt.Errorf("load config: %w", fmt.Errorf("open app.yml: %w", notExist)),
			want: "tool: load config\n  → open app.yml\n  → file does not exist\n",
		},
		{
			name: "a wrapper printing as its wrapped error is skipped",
			tool: "tool",
			err:  spec.Coded(3, fmt.Errorf("load config: %w", spec.Reported(notExist))),
			want: "tool: load config\n  → file does not exist\n",
		},
		{
			name: "a wrapped error in mid-message prints whole and is not descended",
			tool: "tool",
			err:  fmt.Errorf("load config: %w", &quoted{err: fmt.Errorf("open: %w", notExist)}),
			want: "tool: load config\n  → reading \"open: file does not exist\" failed\n",
		},
		{
			name: "a format wrapping several errors prints whole",
			tool: "tool",
			err:  fmt.Errorf("load: %w, %w", errors.New("a"), errors.New("b")),
			want: "tool: load: a, b\n",
		},
		{
			name: "a layer with no text of its own is dropped",
			tool: "tool",
			err:  fmt.Errorf("load config: %w", fmt.Errorf(": %w", notExist)),
			want: "tool: load config\n  → file does not exist\n",
		},
		{
			name: "a usage error prints whole as one row",
			tool: "tool",
			err:  &cobra.UsageError{Path: "remote add", Err: fmt.Errorf("flag: %w", syntax)},
			want: "tool: remote add: flag: strconv: invalid syntax\n",
		},
		{
			name: "a join with no wrapper is summarised by its count",
			tool: "tool",
			err:  errors.Join(errors.New("a"), errors.New("b")),
			want: "tool: 2 errors\n  • a\n  • b\n",
		},
		{
			name: "a join of one error prints as that error",
			tool: "tool",
			err:  fmt.Errorf("load config: %w", errors.Join(fmt.Errorf("open app.yml: %w", notExist))),
			want: "tool: load config\n  → open app.yml\n  → file does not exist\n",
		},
		{
			name: "a wrapped join hangs its errors under the wrapper",
			tool: "tool",
			err: fmt.Errorf("validate config: %w", errors.Join(
				errors.New("name: required"),
				fmt.Errorf("port: out of range: %w", syntax),
			)),
			want: "tool: validate config\n" +
				"  • name: required\n" +
				"  • port: out of range\n" +
				"    → strconv: invalid syntax\n",
		},
		{
			name: "a join under an inner layer nests under that layer",
			tool: "tool",
			err: fmt.Errorf("load: %w", fmt.Errorf("validate: %w",
				errors.Join(errors.New("a"), errors.New("b")))),
			want: "tool: load\n  → validate\n    • a\n    • b\n",
		},
		{
			name: "a nested join nests one level further",
			tool: "tool",
			err: errors.Join(
				errors.New("a"),
				fmt.Errorf("b: %w", errors.Join(errors.New("c"), fmt.Errorf("d: %w", syntax))),
				errors.Join(errors.New("e"), errors.New("f")),
			),
			want: "tool: 3 errors\n" +
				"  • a\n" +
				"  • b\n" +
				"    • c\n" +
				"    • d\n" +
				"      → strconv: invalid syntax\n" +
				"  • 2 errors\n" +
				"    • e\n" +
				"    • f\n",
		},
		{
			name:      "a renderer takes over the top error",
			tool:      "tool",
			err:       field,
			renderers: []errrender.Renderer{renderAs("bad port")},
			want:      "tool: bad port\n",
		},
		{
			name:      "a renderer takes over a layer mid-chain and everything beneath it",
			tool:      "tool",
			err:       fmt.Errorf("load: %w", spec.Coded(3, field)),
			renderers: []errrender.Renderer{renderAs("bad port")},
			want:      "tool: load\n  → bad port\n",
		},
		{
			name: "a renderer takes over one error of a join",
			tool: "tool",
			err: fmt.Errorf("validate: %w", errors.Join(
				errors.New("name: required"), fmt.Errorf("check: %w", field))),
			renderers: []errrender.Renderer{renderAs("bad port")},
			want:      "tool: validate\n  • name: required\n  • check\n    → bad port\n",
		},
		{
			name:      "a renderer returning nothing leaves the default rendering",
			tool:      "tool",
			err:       fmt.Errorf("load: %w", field),
			renderers: []errrender.Renderer{renderAs("")},
			want:      "tool: load\n  → port\n  → strconv: invalid syntax\n",
		},
		{
			name:      "the first renderer that renders an error wins",
			tool:      "tool",
			err:       field,
			renderers: []errrender.Renderer{renderAs(""), renderAs("first"), renderAs("second")},
			want:      "tool: first\n",
		},
		{
			name: "renderer lines after the first are detail rows",
			tool: "tool",
			err: fmt.Errorf("validate: %w", errors.Join(
				errors.New("name: required"), field)),
			renderers: []errrender.Renderer{renderField},
			want: "tool: validate\n" +
				"  • name: required\n" +
				"  • field port\n" +
				"    problem: strconv: invalid syntax\n",
		},
		{
			name:      "detail rows of the top error indent under the summary",
			tool:      "tool",
			err:       field,
			renderers: []errrender.Renderer{renderField},
			want:      "tool: field port\n  problem: strconv: invalid syntax\n",
		},
		{
			name: "a nil error writes nothing",
			tool: "tool",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			errrender.Render(&out, tt.tool, tt.err, tt.renderers)
			if got := out.String(); got != tt.want {
				t.Errorf("Render =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

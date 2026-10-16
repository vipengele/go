package table

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"go.vipengele.org/cli/spec"
)

// gutter separates adjacent columns.
const gutter = "  "

// Column defines one column of a [Table].
type Column struct {
	// Key names the column's value in a [Row]'s JSON object.
	Key string
	// Header is the column's heading in text, written bold.
	Header string
	// Right aligns the column's text to the right; otherwise it is aligned to the left.
	Right bool
	// Style styles a text cell. It receives the unpadded cell text and must write the same
	// visible text with colour on as with colour off. Nil writes the text unstyled.
	Style func(spec.Styler, string) string
}

// Table holds rows of values under a fixed set of columns.
type Table struct {
	cols []Column
	keys []string
	rows [][]any
}

// New returns an empty Table with the columns cols, in order. It panics when cols is empty.
func New(cols ...Column) *Table {
	if len(cols) == 0 {
		panic("table: New: want at least 1 column, got 0")
	}
	keys := make([]string, len(cols))
	for i, c := range cols {
		keys[i] = c.Key
	}
	return &Table{cols: slices.Clone(cols), keys: keys}
}

// Add appends a row holding values, one per column, in column order. It panics when the number
// of values is not the number of columns.
func (t *Table) Add(values ...any) {
	if len(values) != len(t.cols) {
		panic(fmt.Sprintf("table: Add: want %d values, one per column, got %d", len(t.cols), len(values)))
	}
	t.rows = append(t.rows, slices.Clone(values))
}

// Rows returns the rows added so far, in order, for JSON output. It is empty and non-nil for a
// table with no rows, so it marshals as [] rather than null.
func (t *Table) Rows() []Row {
	rows := make([]Row, len(t.rows))
	for i, values := range t.rows {
		rows[i] = Row{keys: t.keys, values: values}
	}
	return rows
}

// Render writes the table to w as text: the header line, then one line per row, each ending in a
// newline. st styles the headers and every [Column.Style]. The output is written in one call to w.
func (t *Table) Render(w io.Writer, st spec.Styler) error {
	header := make([]cell, len(t.cols))
	for i, c := range t.cols {
		header[i] = newCell(st, spec.Styler.Bold, c.Header)
	}
	lines := [][]cell{header}
	for _, values := range t.rows {
		line := make([]cell, len(t.cols))
		for i, c := range t.cols {
			line[i] = newCell(st, c.Style, fmt.Sprint(values[i]))
		}
		lines = append(lines, line)
	}

	widths := make([]int, len(t.cols))
	for _, line := range lines {
		for i, c := range line {
			widths[i] = max(widths[i], c.width)
		}
	}

	var out bytes.Buffer
	for _, line := range lines {
		t.writeLine(&out, line, widths)
	}
	if _, err := w.Write(out.Bytes()); err != nil {
		return fmt.Errorf("table: write: %w", err)
	}
	return nil
}

// writeLine writes one line of cells padded to widths. Spaces are held back until visible text
// follows them, so a line never ends in a gutter or padding, whatever the alignment of its last
// columns.
func (t *Table) writeLine(out *bytes.Buffer, line []cell, widths []int) {
	pending := 0
	for i, c := range line {
		if i > 0 {
			pending += len(gutter)
		}
		pad := widths[i] - c.width
		if t.cols[i].Right {
			pending += pad
		}
		if c.text != "" {
			out.WriteString(strings.Repeat(" ", pending))
			out.WriteString(c.text)
			pending = 0
		}
		if !t.cols[i].Right {
			pending += pad
		}
	}
	out.WriteByte('\n')
}

// cell is a text cell as rendered, with the width it occupies on screen.
type cell struct {
	text  string
	width int
}

// controlSpaces maps each tab, newline and carriage return to one space.
func controlSpaces(r rune) rune {
	switch r {
	case '\t', '\n', '\r':
		return ' '
	default:
		return r
	}
}

// newCell styles text with style under st, and measures it as style writes it with colour off.
// A nil style leaves text unstyled.
func newCell(st spec.Styler, style func(spec.Styler, string) string, text string) cell {
	text = strings.Map(controlSpaces, text)
	if style == nil {
		return cell{text: text, width: utf8.RuneCountInString(text)}
	}
	return cell{text: style(st, text), width: utf8.RuneCountInString(style(spec.Styler{}, text))}
}

// Row is one row of a [Table] as JSON output sees it.
type Row struct {
	keys   []string
	values []any
}

// MarshalJSON writes the row as a JSON object holding each column's [Column.Key] and value, in
// column order. Each value is marshalled with [encoding/json] as it was added.
func (r Row) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range r.keys {
		if i > 0 {
			out.WriteByte(',')
		}
		k, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("table: marshal key %q: %w", key, err)
		}
		v, err := json.Marshal(r.values[i])
		if err != nil {
			return nil, fmt.Errorf("table: marshal value of %q: %w", key, err)
		}
		out.Write(k)
		out.WriteByte(':')
		out.Write(v)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

package table_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/speccheck"
	"go.vipengele.org/cli/spec/table"
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

func colourOn() spec.Styler { return spec.Output{}.Styler(spec.Stdout) }

// render renders tbl with st and fails t on an error.
func render(t *testing.T, tbl *table.Table, st spec.Styler) string {
	t.Helper()
	var buf bytes.Buffer
	if err := tbl.Render(&buf, st); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return buf.String()
}

// build returns a table with cols holding rows.
func build(cols []table.Column, rows [][]any) *table.Table {
	tbl := table.New(cols...)
	for _, r := range rows {
		tbl.Add(r...)
	}
	return tbl
}

func lines(s ...string) string { return strings.Join(s, "\n") + "\n" }

func TestRenderAlignsColumns(t *testing.T) {
	tests := []struct {
		name string
		cols []table.Column
		rows [][]any
		want string
	}{
		{
			name: "left then right",
			cols: []table.Column{{Key: "name", Header: "NAME"}, {Key: "size", Header: "SIZE", Right: true}},
			rows: [][]any{{"a", 1}, {"bbbb", 100}},
			want: lines(
				"NAME  SIZE",
				"a        1",
				"bbbb   100",
			),
		},
		{
			name: "right then left",
			cols: []table.Column{{Key: "size", Header: "SIZE", Right: true}, {Key: "name", Header: "NAME"}},
			rows: [][]any{{1, "a"}, {100, "bbbb"}},
			want: lines(
				"SIZE  NAME",
				"   1  a",
				" 100  bbbb",
			),
		},
		{
			name: "cell wider than header",
			cols: []table.Column{{Key: "id", Header: "ID"}, {Key: "path", Header: "PATH"}},
			rows: [][]any{{"abcdef", "/x"}, {"g", "/yy"}},
			want: lines(
				"ID      PATH",
				"abcdef  /x",
				"g       /yy",
			),
		},
		{
			name: "multi-byte runes",
			cols: []table.Column{{Key: "n", Header: "N"}, {Key: "v", Header: "V", Right: true}},
			rows: [][]any{{"café", 1}, {"x", 22}, {"ñandú", 3}},
			want: lines(
				"N       V",
				"café    1",
				"x      22",
				"ñandú   3",
			),
		},
		{
			name: "empty trailing cells",
			cols: []table.Column{{Key: "a", Header: "A"}, {Key: "b", Header: "B"}, {Key: "c", Header: "C", Right: true}},
			rows: [][]any{{"long", "", ""}, {"x", "y", ""}},
			want: lines(
				"A     B  C",
				"long",
				"x     y",
			),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(t, build(tc.cols, tc.rows), spec.Styler{}); got != tc.want {
				t.Errorf("Render =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestEmptyTable(t *testing.T) {
	tbl := table.New(table.Column{Key: "name", Header: "NAME"}, table.Column{Key: "size", Header: "SIZE", Right: true})

	if got, want := render(t, tbl, spec.Styler{}), "NAME  SIZE\n"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
	rows := tbl.Rows()
	if rows == nil {
		t.Error("Rows() = nil, want an empty non-nil slice")
	}
	if len(rows) != 0 {
		t.Errorf("len(Rows()) = %d, want 0", len(rows))
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal Rows(): %v", err)
	}
	if got, want := string(data), "[]"; got != want {
		t.Errorf("Rows() marshals to %s, want %s", got, want)
	}
}

// styledColumns are columns whose styles add visible glyphs and colour.
func styledColumns() []table.Column {
	return []table.Column{
		{Key: "status", Header: "STATUS", Style: spec.Styler.Success},
		{Key: "note", Header: "NOTE", Style: spec.Styler.Dim},
		{Key: "count", Header: "COUNT", Right: true, Style: spec.Styler.Warning},
	}
}

func styledRows() [][]any {
	return [][]any{{"ok", "fine", 1}, {"failed", "", 10}, {"new", "long note", 100}}
}

func TestGlyphStyleKeepsColumnsAligned(t *testing.T) {
	want := lines(
		"STATUS    NOTE       COUNT",
		"✓ ok      fine         ⚠ 1",
		"✓ failed              ⚠ 10",
		"✓ new     long note  ⚠ 100",
	)
	tests := []struct {
		name string
		st   spec.Styler
	}{
		{"colour off", spec.Styler{}},
		{"colour on", colourOn()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripSGR(render(t, build(styledColumns(), styledRows()), tc.st))
			if got != want {
				t.Errorf("Render, SGR stripped =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestColourOnStripsToColourOff(t *testing.T) {
	tests := []struct {
		name string
		cols []table.Column
		rows [][]any
	}{
		{"styled", styledColumns(), styledRows()},
		{"unstyled", []table.Column{{Key: "a", Header: "A"}, {Key: "b", Header: "B", Right: true}}, [][]any{{"x", 1}}},
		{"empty", styledColumns(), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			on := render(t, build(tc.cols, tc.rows), colourOn())
			off := render(t, build(tc.cols, tc.rows), spec.Styler{})
			if !strings.Contains(on, "\x1b[") {
				t.Errorf("colour-on output %q carries no SGR sequence", on)
			}
			if strings.Contains(off, "\x1b") {
				t.Errorf("colour-off output %q carries an escape", off)
			}
			if got := stripSGR(on); got != off {
				t.Errorf("stripped colour-on output =\n%s\nwant colour-off output\n%s", got, off)
			}
		})
	}
}

func TestEveryLineHasTheSameVisibleWidth(t *testing.T) {
	tests := []struct {
		name string
		st   spec.Styler
	}{
		{"colour off", spec.Styler{}},
		{"colour on", colourOn()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cols := []table.Column{
				{Key: "name", Header: "NAME", Style: spec.Styler.Info},
				{Key: "where", Header: "WHERE"},
				{Key: "size", Header: "SIZE", Right: true, Style: spec.Styler.Error},
			}
			rows := [][]any{{"a", "café", 1}, {"longer name", "x", 12345}, {"b", "", 7}}
			out := strings.TrimSuffix(stripSGR(render(t, build(cols, rows), tc.st)), "\n")
			var widths []int
			for line := range strings.SplitSeq(out, "\n") {
				widths = append(widths, utf8.RuneCountInString(line))
			}
			for i, w := range widths {
				if w != widths[0] {
					t.Errorf("line %d is %d runes wide, line 0 is %d:\n%s", i, w, widths[0], out)
				}
			}
		})
	}
}

func TestStyleSeesUnpaddedCellText(t *testing.T) {
	var seen []string
	down := func(st spec.Styler, text string) string {
		seen = append(seen, text)
		if text == "down" {
			return st.Error(text)
		}
		return text
	}
	cols := []table.Column{{Key: "state", Header: "STATE", Style: down}, {Key: "n", Header: "N", Right: true}}
	rows := [][]any{{"up", 1}, {"down", 2}}

	got := render(t, build(cols, rows), spec.Styler{})
	want := lines(
		"STATE   N",
		"up      1",
		"✗ down  2",
	)
	if got != want {
		t.Errorf("Render =\n%s\nwant\n%s", got, want)
	}
	if len(seen) == 0 {
		t.Fatal("Style was never called")
	}
	for _, text := range seen {
		if text != "up" && text != "down" {
			t.Errorf("Style received %q, want the unpadded cell text", text)
		}
	}
	if !slices.Contains(seen, "down") {
		t.Errorf("Style received %q, none of them %q", seen, "down")
	}
}

func TestControlCharactersBecomeSpacesInText(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"tab", "a\tb", lines("V    N", "a b  1")},
		{"newline", "a\nb", lines("V    N", "a b  1")},
		{"carriage return", "a\rb", lines("V    N", "a b  1")},
		{"each one its own space", "a\r\n\tb", lines("V      N", "a   b  1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tbl := table.New(table.Column{Key: "v", Header: "V"}, table.Column{Key: "n", Header: "N"})
			tbl.Add(tc.value, 1)

			if got := render(t, tbl, spec.Styler{}); got != tc.want {
				t.Errorf("Render = %q, want %q", got, tc.want)
			}

			data, err := json.Marshal(tbl.Rows()[0])
			if err != nil {
				t.Fatalf("marshal row: %v", err)
			}
			var back map[string]any
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal %s: %v", data, err)
			}
			if back["v"] != tc.value {
				t.Errorf("JSON value = %q, want %q unchanged", back["v"], tc.value)
			}
		})
	}
}

func TestRowJSONIsInColumnOrderWithTypedValues(t *testing.T) {
	tbl := table.New(
		table.Column{Key: "zeta", Header: "Z"},
		table.Column{Key: "alpha", Header: "A"},
		table.Column{Key: "mid", Header: "M"},
		table.Column{Key: "tags", Header: "T"},
		table.Column{Key: "none", Header: "N"},
	)
	tbl.Add("x", 3, true, []string{"a", "b"}, nil)
	tbl.Add("y\tz", 2.5, false, []string{}, map[string]int{"k": 1})

	tests := []struct {
		name string
		row  int
		want string
	}{
		{"strings, numbers, booleans and null", 0, `{"zeta":"x","alpha":3,"mid":true,"tags":["a","b"],"none":null}`},
		{"nested values", 1, `{"zeta":"y\tz","alpha":2.5,"mid":false,"tags":[],"none":{"k":1}}`},
	}
	rows := tbl.Rows()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(rows[tc.row])
			if err != nil {
				t.Fatalf("marshal row: %v", err)
			}
			if got := string(data); got != tc.want {
				t.Errorf("row JSON = %s, want %s", got, tc.want)
			}
		})
	}

	if got := render(t, tbl, spec.Styler{}); !strings.Contains(got, "<nil>") {
		t.Errorf("Render = %q, want a nil value written as <nil>", got)
	}
}

func TestRowJSONReportsAValueThatDoesNotMarshal(t *testing.T) {
	tbl := table.New(table.Column{Key: "f", Header: "F"})
	tbl.Add(func() {})
	if _, err := json.Marshal(tbl.Rows()); err == nil {
		t.Error("marshal of a row holding a func succeeded, want an error")
	}
}

func TestPanics(t *testing.T) {
	tests := []struct {
		name string
		call func()
		want []string
	}{
		{"New without columns", func() { table.New() }, []string{"at least 1 column", "got 0"}},
		{"Add with too few values", func() {
			table.New(table.Column{Key: "a"}, table.Column{Key: "b"}).Add("x")
		}, []string{"want 2 values", "got 1"}},
		{"Add with too many values", func() {
			table.New(table.Column{Key: "a"}, table.Column{Key: "b"}).Add("x", "y", "z")
		}, []string{"want 2 values", "got 3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("did not panic")
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic value %T, want string", r)
				}
				for _, want := range tc.want {
					if !strings.Contains(msg, want) {
						t.Errorf("panic message %q does not mention %q", msg, want)
					}
				}
			}()
			tc.call()
		})
	}
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestRenderReportsAWriteError(t *testing.T) {
	tbl := table.New(table.Column{Key: "a", Header: "A"})
	err := tbl.Render(failingWriter{}, spec.Styler{})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("Render error = %v, want one wrapping the write error", err)
	}
}

// The keys of a row are the column keys, so renaming, removing or retyping a column is a contract
// change that increments the command's schema. The table is non-empty: an empty rows slice has no
// element keys to pin.
func TestRowKeys(t *testing.T) {
	tbl := table.New(
		table.Column{Key: "name", Header: "NAME"},
		table.Column{Key: "size", Header: "SIZE", Right: true},
		table.Column{Key: "tags", Header: "TAGS"},
	)
	tbl.Add("alpha", 12, []string{"a"})

	var out bytes.Buffer
	payload := struct {
		Items []table.Row `json:"items"`
	}{Items: tbl.Rows()}
	if err := spec.WriteJSON(&out, 1, payload); err != nil {
		t.Fatalf("spec.WriteJSON() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decoding %s: %v", out.Bytes(), err)
	}
	speccheck.AssertJSONKeys(t, got, "testdata/row-keys.golden")
}

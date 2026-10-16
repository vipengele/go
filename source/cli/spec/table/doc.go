// Package table renders rows as aligned text columns and exposes the same rows as objects for
// JSON output.
//
// A [Table] is built from its [Column] definitions with [New], filled with [Table.Add], and then
// either rendered as text with [Table.Render] or handed to JSON output through [Table.Rows]. The
// table writes no JSON and owns no schema: a command puts the rows under a key of its own payload
// and writes that with [spec.WriteJSON] (docs/adr/0014-json-output-envelope.md). An empty table
// renders its header line only; a "no results" hint is a message, and a command writes it to stderr
// (docs/adr/0013-stdout-carries-data-stderr-carries-messages.md).
//
// # Alignment and colour
//
// Every column is as wide as its widest cell or header. Cells are separated by a two-space gutter,
// and the last column carries no trailing padding: a right-aligned last column is padded on the
// left only, and a line never ends in padding or a gutter, even when its last cells are empty.
// Headers are bold; their case is the caller's.
//
// A cell is measured on what its style writes with colour off, [spec.Styler]'s zero value, and
// rendered with the Styler passed to [Table.Render]; padding is added around the styled text, never
// inside it. A style therefore sees the unpadded cell text, so a style that tests the text matches
// it, and a style that adds visible characters, such as the glyph and space [spec.Styler.Success]
// prepends, widens the column instead of shifting it. Colour on and colour off differ only in SGR
// escape sequences, provided a [Column.Style] writes the same visible text with colour on as with
// it off, as every [spec.Styler] method does.
//
// # Width limits
//
// Width is the number of runes. A rune that a terminal draws two cells wide (East Asian wide
// characters, most emoji), a combining mark that draws no cell of its own, and a ZWJ sequence that
// draws several runes as one glyph are each counted as one cell per rune, so a column holding them
// can misalign.
//
// # Text and JSON values
//
// In text, a cell is its value formatted with [fmt.Sprint], and each tab, newline and carriage
// return in it becomes one space, so a cell never breaks a line. In JSON, a cell is its value as
// [encoding/json] marshals it, unchanged. The two can differ: a nil value is <nil> in text and null
// in JSON, and a value with a String method may print differently from how it marshals.
package table

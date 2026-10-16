# The table helper lives in `cli/spec/table`

Status: accepted. It keeps to the dependency budget of `0009-platform-paths-live-in-cli-spec-paths.md`: the standard library and `cli/spec`.

A command that lists things writes the same rows twice: as aligned text for a person, and as objects
for a program under `--output json`. The public package `go.vipengele.org/cli/spec/table`, in the
`cli/spec` module, holds both. A `Table` is built from `Column` definitions with `New`, filled with
`Add`, and then either rendered with `Render(w, st)` or handed to JSON output through `Rows()`, which
returns `[]Row`. A `Row` marshals as a JSON object with the columns' keys in column order. The table
performs no I/O of its own beyond the writer it is given, writes no JSON and owns no schema.

## The rules

- **The envelope is the command's.** `Rows()` returns the rows; the command puts them under a key of
  its own payload and writes that with `spec.WriteJSON` and its own schema number
  (`0014-json-output-envelope.md`). A command pins its keys with `speccheck.AssertJSONKeys` against a
  non-empty table: an empty `rows` collapses to a different golden, so an empty table does not pin
  the row keys.
- **Width is the rune count.** Padding is hand-rolled with the standard library. A rune a terminal
  draws two cells wide (East Asian wide characters, most emoji), a combining mark and a ZWJ sequence
  are each counted as one cell per rune, so a column holding them can misalign. The package
  documentation names the limit.
- **An empty table prints its header line only** in text, and `Rows()` is an empty non-nil slice, so
  it marshals as `[]`. A "no results" hint is a message and the command writes it to stderr
  (`0013-stdout-carries-data-stderr-carries-messages.md`).
- **Style the unpadded text, pad on the measured width.** A cell is measured on what its style
  writes with colour off (`spec.Styler{}`), rendered with the `Styler` passed to `Render`, and
  padded around the styled text. The style function receives the unpadded cell text. `Styler.Info`,
  `Success`, `Warning` and `Error` prepend a glyph and a space even with colour off, so a glyph that
  is measured widens its column instead of shifting it, and colour on and colour off differ only in
  SGR bytes.
- **`Render` takes a `Styler`.** `Output.Styler(stream)` already makes the per-stream colour
  decision, so the table takes its result and not an `Output` and a `Stream`. Headers are bold and
  their case is the caller's. The gutter is two spaces, and a line never ends in padding or a gutter:
  a right-aligned last column is padded on the left only.
- **Text and JSON take their values separately.** A text cell is its value formatted with
  `fmt.Sprint`; a JSON value is marshalled by `encoding/json` as it was added. A nil value is `<nil>`
  in text and `null` in JSON. In text, a tab, newline or carriage return in a cell becomes one space,
  so a cell never breaks a line; JSON keeps the value as given.
- **A call-site mistake panics.** `New` with no columns and `Add` with a number of values that is not
  the number of columns panic, with a message naming the expected and received counts. The mistake is
  in the code that calls, not in the data; `spec/stream.go` panics on a misconfigured `Env` for the
  same reason.
- **Table only.** A list is a table of one or two columns.

## Considered options

- **The root `spec` package as the home.** It performs no I/O (`AGENTS.md` of `cli/spec`), and
  `Render` writes to a writer, so a table belongs in a subpackage that may.
- **`cli/core` as the home.** `update` and `daemon` do not import `cli/core` (`0008-update-takes-its-identity-from-cli-spec.md`),
  so a command in either could not use the table without a second copy of it.
- **A method on `spec.Invocation`.** It ties a rendering helper to the invocation model and gives
  the root package a layout concern; the table needs a writer and a `Styler`, which a command already
  holds.
- **A helper that writes the envelope.** The helper would own a schema number and a key for rows, and
  a command with other keys beside its rows could not extend the payload. The envelope is flat and
  versioned by the command that owns its keys (`0014-json-output-envelope.md`).
- **A fixed `rows` key.** Two tables in one payload would collide, and a command could not name its
  list for what it holds, such as `releases` or `modules`. The key is the command's.
- **`text/tabwriter`.** It measures the raw cell text, so an SGR escape sequence in a styled cell
  counts as visible width and colour alters alignment, and a glyph a style prepends cannot be
  measured apart from the style. Its `AlignRight` flag applies to tab-terminated cells, so the last
  column cannot be right-aligned without a trailing cell.
- **A width library such as `go-runewidth` or `lipgloss`.** A third-party module in every
  consumer's graph, for a limit the package documents and a command with East Asian text can work
  around with its own cell text. `0009-platform-paths-live-in-cli-spec-paths.md` rejects the same
  trade for `adrg/xdg`.
- **Styling after padding.** The glyph a `Styler` method prepends shifts every column after it, and
  a style that tests the cell text never matches a padded cell. A test that colour on equals colour
  off once SGR is stripped passes while alignment is broken, so the tests also use a glyph-adding
  style and check that every line has the same visible width.
- **Stripping ANSI from pre-styled cells.** A caller passes a plain value and a style, and the table
  never receives escape sequences to measure around. Stripping needs a parser for sequences the
  table does not otherwise handle, and misreads text that contains one.
- **An error-returning `Add`.** An arity mismatch is fixed at the call site and never depends on
  input, and an error return puts an `errcheck` in every row loop for a failure that cannot occur in
  a correct program.
- **`[]map[string]any` for the rows.** Go marshals map keys sorted, so the columns would lose their
  order. `Row` keeps the order.

## Consequences

- **`cli/spec` gains a public subpackage** with no dependency beyond the standard library and
  `cli/spec`. A change to it selects `cli/core` and `update` in CI
  (`0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md`).
- **A `Column.Style` must write the same visible text with colour on as with colour off**, as every
  `Styler` method does. A style that adds text only with colour on misaligns the colour-on output.
- **A command with East Asian or emoji text may misalign**, and the table gives it nothing to
  correct that.
- **Each `Render` is one write to the writer.** The table is built in memory first, so a failed write
  reports one error and a partial table is the writer's concern.

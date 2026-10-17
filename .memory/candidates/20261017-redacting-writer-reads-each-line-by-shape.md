---
about: "the cli/core RedactingWriter picks the header pattern per line by its shape (valid JSON or not), not by --log-format, and a line emitted in pieces fixes its shape from the first piece"
saw:
  - source/cli/core/logging/redact.go
  - source/cli/core/logging/redact_writer.go
  - source/cli/core/logging/logging.go
  - docs/adr/0018-logging-is-an-opt-in-app-option.md
---

- The writer also carries raw and subprocess output (`cli.LogWriterFrom`), which is not in the log
  format. A pattern chosen from the format leaked: a plain `Set-Cookie: sid="abc"` line under the JSON
  format stopped at the first quote, and a raw `{"Cookie":"..."}` line under the text format never
  matched. `lineShape` / `shapeOf` (`redact.go`) read a whole line as JSON only when it is valid JSON
  starting with `{` or `[`; the text pattern also matches the quoted key form `"Name": value`.
- A line written in pieces (a 64 KiB overflow, or `Flush`) cannot be validated whole, so
  `shapeOfStart` decides from what is held: JSON only for a whole JSON line or the start of a JSON
  document (a `json.Decoder` token run that ends in EOF), text otherwise. Deciding from a leading `{`
  alone leaked `{Cookie: sid="abc"` and Go `%+v` dumps.
- Both layers run over the same bytes (the handler redacts, then the writer sees the formatted line),
  so redaction must be idempotent: `[REDACTED]` is neither a token nor a header name.
- Known leaks: a key written with escapes inside the quotes (the writer matches only the plain string
  `"Name"`), text after a complete JSON document within one line when the line arrives in pieces, and a
  `Flush` that ends between `"Name":` and its value. A header whose JSON value is not a string
  (`{"Cookie":["sid=abc"]}`, an object, a number) is redacted: `jsonKey` finds the key and `jsonSkip`
  scans the value to its end.

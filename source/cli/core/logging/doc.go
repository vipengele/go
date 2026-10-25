// Package logging builds the process-wide [slog.Logger] of a command-line program from a level
// name and an output [spec.Format], and redacts secrets from everything it writes.
//
// Redaction has two layers, both built from one [Redaction] and both always present. A
// [slog.Handler] wrapper redacts the message and attributes of every record before the handler
// formats it, and a [RedactingWriter] beneath the handler redacts the lines it writes, along with
// any other text a caller writes to the same destination through it.
//
// # Field names
//
// Attribute keys are lower-case, short and stable across calls, so a log query written against one
// call site finds the others.
//
//   - Related fields go in a [slog.Group]: "http.status", not "http_status".
//   - The error goes under "err".
//   - A duration goes under "elapsed" as a [time.Duration].
//   - A message is constant. Variable data goes in attributes, so a test asserts on the message and
//     on the keys without parsing text.
//
// Redaction is a backstop, not permission to log secrets. The value of an attribute whose key is a
// header name, such as "Authorization", is replaced whatever the letter case of the key and at any
// group depth, and the configured token prefixes are replaced in the text of messages and values.
// A secret under any other key, with no known prefix, is written as it is.
//
// # Levels
//
//   - debug is detail only a developer diagnosing a problem wants.
//   - info is a state change the user asked for or would expect to be told about.
//   - warn is something recoverable that the user might want to fix.
//   - error is an operation that failed. It is logged once, where it is handled, and not at every
//     layer it passes through.
//
// Tests assert on records with the package go.vipengele.org/cli/core/logging/logtest.
package logging

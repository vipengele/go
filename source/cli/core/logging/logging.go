// Package logging builds the process-wide [slog.Logger] of a command-line program from a level
// name and an output [spec.Format], and redacts secrets from everything it writes.
//
// Redaction has two layers, both built from one [Redaction] and both always present. A
// [slog.Handler] wrapper redacts the message and attributes of every record before the handler
// formats it, and a [RedactingWriter] beneath the handler redacts the lines it writes, along with
// any other text a caller writes to the same destination through it.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.vipengele.org/cli/spec"
)

// Options configures [Setup].
type Options struct {
	// Level is the minimum level a record needs to be written.
	Level slog.Level
	// Format selects the text or JSON handler. The zero value is [spec.FormatText].
	Format spec.Format
	// Writer receives the records. A nil Writer means [os.Stderr].
	Writer io.Writer
	// Redaction names the secrets redacted in addition to the baseline of every [Redaction]. The
	// zero value redacts the baseline alone.
	Redaction Redaction
}

// Result is what [Setup] builds.
type Result struct {
	// Logger is the logger Setup installs as the [slog] default.
	Logger *slog.Logger
	// Writer is the redacting writer between Logger's handler and Options.Writer, reading lines
	// in Options.Format. Text written to it is redacted like the records of Logger; it holds the
	// bytes after the last newline until a later write completes the line or until
	// [RedactingWriter.Flush].
	Writer *RedactingWriter
}

// ParseLevel returns the level named by s: debug, info, warn or error, in any letter case.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q: want debug, info, warn or error", s)
}

// Setup builds a logger from opts, installs it as the [slog] default and returns it with the
// redacting writer it writes through. One redactor, built from opts.Redaction, serves both
// layers: the handler redacts each record's message and attributes, and the writer redacts each
// line on its way to opts.Writer. Setup returns an error, and leaves the default untouched, when
// opts.Format is not [spec.FormatText] or [spec.FormatJSON].
func Setup(opts Options) (Result, error) {
	var mode redactMode
	switch opts.Format {
	case spec.FormatText:
		mode = redactText
	case spec.FormatJSON:
		mode = redactJSON
	default:
		return Result{}, fmt.Errorf("unsupported log format %q", opts.Format.String())
	}

	dst := opts.Writer
	if dst == nil {
		dst = os.Stderr
	}
	r := newRedactor(opts.Redaction)
	w := newRedactingWriter(dst, r, mode)
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}
	var h slog.Handler
	if mode == redactJSON {
		h = slog.NewJSONHandler(w, handlerOpts)
	} else {
		h = slog.NewTextHandler(w, handlerOpts)
	}
	logger := slog.New(newRedactingHandler(h, r))
	slog.SetDefault(logger)
	return Result{Logger: logger, Writer: w}, nil
}

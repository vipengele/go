// Package logging builds the process-wide [slog.Logger] of a command-line program from a level
// name and an output [spec.Format].
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

// Setup builds a logger from opts, installs it as the [slog] default and returns it. It returns
// an error, and leaves the default untouched, when opts.Format is not [spec.FormatText] or
// [spec.FormatJSON].
func Setup(opts Options) (*slog.Logger, error) {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}
	var h slog.Handler
	switch opts.Format {
	case spec.FormatText:
		h = slog.NewTextHandler(w, handlerOpts)
	case spec.FormatJSON:
		h = slog.NewJSONHandler(w, handlerOpts)
	default:
		return nil, fmt.Errorf("unsupported log format %q", opts.Format.String())
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger, nil
}

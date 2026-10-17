package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/core/logging"
	"go.vipengele.org/cli/spec"
)

// WithLogging gives the App the flags --log-level and --log-format, which configure the
// process-wide logger the go.vipengele.org/cli/core/logging package builds. Both are declared on
// the root and inherited by every command, as [WithVerbose] describes, after the standard flags.
//
// --log-level names the minimum level of a record: debug, info, warn or error. --log-format names
// the format of the records, text or json, the spellings of [WithOutput], and is text when the
// command line does not set it.
//
// Before every run function, after the configuration hook of [WithConfig] and ahead of every
// [WithPreRun] hook, a pre-run hook installs the logger with [logging.Setup], writing to the
// env.Stderr of the execution, and stores it in the context it hands on, where [LoggerFrom] reads
// it. The level is info, debug under -v, error under -q, and the one --log-level names when the
// command line sets it. --log-level set beside -v or -q, a --log-level other than debug, info,
// warn or error, in any letter case, and a --log-format other than text or json are usage errors:
// the hook writes the problem, naming the flag, and the execution returns [ExitUsage] without
// calling the run function.
//
// The logger the hook installs is the process-wide [slog.Default], so executions that overlap in
// one process, of one App or of two, share the one installed last, and a record written through
// the default may reach the env.Stderr of another execution. Code that needs the logger of its own
// execution reads it with [LoggerFrom].
//
// The App also needs [WithVerbose] and [WithQuiet]: without either, every execution returns
// [ExitError] after writing "<name>: invalid command tree" to env.Stderr, followed by a row naming
// the options it lacks, and calls no run function.
func WithLogging() Option {
	return func(s *settings) { s.logging = true }
}

// WithRedaction adds the secrets of a tool to the ones the logger of [WithLogging] redacts: the
// prefixes of its tokens and the names of its headers, as [logging.Redaction] describes. The
// baseline of [logging.Redaction] is redacted whether or not the option is given, and a tool adds
// to it and cannot remove from it. The option can be given more than once; the prefixes and the
// header names of every call apply, in the order given. r is copied, so a caller changing its
// slices afterwards changes nothing.
//
// The option needs [WithLogging]: without it, every execution returns [ExitError] after writing
// "<name>: invalid command tree" to env.Stderr, followed by a row naming the option it lacks, and
// calls no run function. The redaction covers the records of the logger and whatever a run function
// writes to the writer [LogWriterFrom] returns.
func WithRedaction(r logging.Redaction) Option {
	return func(s *settings) {
		s.redaction.Prefixes = append(s.redaction.Prefixes, r.Prefixes...)
		s.redaction.Headers = append(s.redaction.Headers, r.Headers...)
		s.redactionSet = true
	}
}

// loggingFlags holds the handles of the flags [WithLogging] declares for one execution. Both
// handles are nil when the App does not take the option.
type loggingFlags struct {
	// redaction is the tool's addition to the baseline, a copy of the App's.
	redaction logging.Redaction
	// writer holds the redacting writer the hook installs, for flush to find when the execution
	// ends. It is nil when the App does not take the option.
	writer *writerSlot

	// level declares no default: the level a command line that does not set --log-level runs at
	// follows -v and -q, so a declared default would show in --help as a level that does not
	// apply, and "" reads unambiguously as not given beside [spec.FlagOf.IsSet].
	level  *spec.FlagOf[string]
	format *spec.FlagOf[string]
}

// writerSlot holds the [*logging.RedactingWriter] of one execution. The hook stores it on the
// goroutine that runs the command and flush reads it on the one that called Execute, which may
// have abandoned the run.
type writerSlot struct {
	mu sync.Mutex
	w  *logging.RedactingWriter
}

// newLoggingFlags returns the handles of the flags [WithLogging] declares, or none when s does
// not opt into it.
func newLoggingFlags(s settings) loggingFlags {
	if !s.logging {
		return loggingFlags{}
	}
	return loggingFlags{
		redaction: logging.Redaction{
			Prefixes: append([]string(nil), s.redaction.Prefixes...),
			Headers:  append([]string(nil), s.redaction.Headers...),
		},
		writer: &writerSlot{},
		level: spec.StringFlag("log-level", "write log records at or above debug, info, warn or error",
			spec.Inherited()),
		format: spec.StringFlag("log-format", "write log records as text or json",
			spec.Default(spec.FormatText.String()), spec.Inherited()),
	}
}

// hook returns the pre-run hook of [WithLogging], which reads the level from f and from verbose
// and quiet, the -v and -q of the execution, or nil when the App does not take the option.
func (f loggingFlags) hook(verbose, quiet *spec.FlagOf[bool]) PreRunHook {
	if f.level == nil {
		return nil
	}
	return func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
		// Setup fails only on a format other than text or json, which is a problem of the command
		// line like every one options reports, so both are one usage error.
		opts, err := f.options(inv, verbose, quiet)
		var result logging.Result
		if err == nil {
			result, err = logging.Setup(opts)
		}
		if err != nil {
			return ctx, &cobra.UsageError{Path: commandPath(cmd), Err: err}
		}
		f.writer.mu.Lock()
		f.writer.w = result.Writer
		f.writer.mu.Unlock()
		ctx = context.WithValue(ctx, loggerKey{}, result.Logger)
		return context.WithValue(ctx, logWriterKey{}, result.Writer), nil
	}
}

// flush writes the bytes the execution's redacting writer still holds after the last newline to
// the env.Stderr it wraps, or does nothing when the hook installed none. The App calls it when the
// execution ends, whatever the outcome. An error of the destination is dropped: the execution is
// over and has nowhere to report it.
func (f loggingFlags) flush() {
	if f.writer == nil {
		return
	}
	f.writer.mu.Lock()
	w := f.writer.w
	f.writer.mu.Unlock()
	if w != nil {
		_ = w.Flush()
	}
}

// options returns the [logging.Options] the command line of inv selects, writing to the
// execution's env.Stderr, or the problem of a command line that sets a level twice or names a
// level or a format that does not exist.
func (f loggingFlags) options(inv *spec.Invocation, verbose, quiet *spec.FlagOf[bool]) (logging.Options, error) {
	opts := logging.Options{Level: slog.LevelInfo, Writer: inv.Env.Stderr, Redaction: f.redaction}
	switch {
	case f.level.IsSet(inv):
		if isSet(verbose, inv) {
			return opts, errors.New("--log-level and --verbose cannot be used together")
		}
		if isSet(quiet, inv) {
			return opts, errors.New("--log-level and --quiet cannot be used together")
		}
		value := f.level.Get(inv)
		level, err := logging.ParseLevel(value)
		if err != nil {
			return opts, fmt.Errorf("--log-level must be debug, info, warn or error, not %q", value)
		}
		opts.Level = level
	case isSet(verbose, inv):
		opts.Level = slog.LevelDebug
	case isSet(quiet, inv):
		opts.Level = slog.LevelError
	}
	switch value := f.format.Get(inv); value {
	case spec.FormatText.String():
		opts.Format = spec.FormatText
	case spec.FormatJSON.String():
		opts.Format = spec.FormatJSON
	default:
		return opts, fmt.Errorf("--log-format must be %s or %s, not %q", spec.FormatText, spec.FormatJSON, value)
	}
	return opts, nil
}

// loggerKey is the context key under which the hook of [WithLogging] stores the [*slog.Logger] it
// installs.
type loggerKey struct{}

// LoggerFrom returns the logger the hook of [WithLogging] installed for the command that runs,
// from the context its run function, a later [PreRunHook] or a [PostRunHook] receives, and whether
// the context carries one. It reports false when the App has no [WithLogging].
func LoggerFrom(ctx context.Context) (*slog.Logger, bool) {
	logger, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	return logger, ok
}

// logWriterKey is the context key under which the hook of [WithLogging] stores the
// [*logging.RedactingWriter] beneath the logger it installs.
type logWriterKey struct{}

// LogWriterFrom returns the redacting writer beneath the logger the hook of [WithLogging]
// installed, and whether the context carries one, from the same contexts as [LoggerFrom]. It
// reports false when the App has no [WithLogging]. Text written to it is redacted like the records
// of the logger and reaches the env.Stderr of the execution in the log format, line by line; the
// App flushes what it still holds, a line without its newline, when the execution returns.
func LogWriterFrom(ctx context.Context) (*logging.RedactingWriter, bool) {
	w, ok := ctx.Value(logWriterKey{}).(*logging.RedactingWriter)
	return w, ok
}

// commandPath returns the path of cmd with the root's name left out, as a usage error of the
// command line names the command: "" for the root, "remote add" for a grandchild. Every name of
// a valid tree is a slug, so the first space of the joined path ends the root's name; a path of
// the root alone, or the empty path of a zero [spec.CommandInfo], has none and yields "".
func commandPath(cmd spec.CommandInfo) string {
	_, rest, _ := strings.Cut(cmd.String(), " ")
	return rest
}

// chainHooks returns a pre-run hook that calls the non-nil hooks of hs in order, each receiving
// the context the one before it returned, a nil one keeping the context it was given, and stops
// at the first error. It returns nil when every hook of hs is nil.
func chainHooks(hs ...PreRunHook) PreRunHook {
	var chain []PreRunHook
	for _, h := range hs {
		if h != nil {
			chain = append(chain, h)
		}
	}
	if len(chain) == 0 {
		return nil
	}
	return func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
		for _, h := range chain {
			next, err := h(ctx, inv, cmd)
			if err != nil {
				return ctx, err
			}
			if next != nil {
				ctx = next
			}
		}
		return ctx, nil
	}
}

// list returns the flags the root declares for [WithLogging], in the order --log-level,
// --log-format.
func (f loggingFlags) list() []spec.Flag {
	if f.level == nil {
		return nil
	}
	return []spec.Flag{f.level, f.format}
}

// loggingProblem returns the problem of options s that hold [WithLogging] without both
// [WithVerbose] and [WithQuiet], or [WithRedaction] without [WithLogging], and nil for any other
// options. The two cannot apply together, as the first needs the logging the second lacks.
func loggingProblem(s settings) error {
	switch {
	case s.logging && (!s.verbose || !s.quiet):
		return errors.New("WithLogging requires WithVerbose and WithQuiet")
	case s.redactionSet && !s.logging:
		return errors.New("WithRedaction requires WithLogging")
	}
	return nil
}

# Logging is an opt-in App option

Status: accepted. It builds on `0016-pre-run-hooks-return-the-context.md` and on
`0013-stdout-carries-data-stderr-carries-messages.md`, whose quiet it follows.

A tool built on `cli.App` passes `cli.WithLogging()` and gets the process-wide `slog` logger
configured from the command line by two inherited flags, `--log-level` and `--log-format`. Without
the option the App declares neither flag and installs no logger. The logger itself is built by the
public package `go.vipengele.org/cli/core/logging`.

## Decisions

- **Flags.** `--log-level` takes debug, info, warn or error in any letter case. `--log-format` takes
  `text` or `json`, the spellings of `--output`, and is `text` when not given. Both are inherited by
  every command, after the standard flags.
- **Required options.** `WithLogging` needs `WithVerbose` and `WithQuiet`, because the level follows
  them. Without either, the command tree is invalid and every execution fails before a run function.
- **Level.** The default is info. `-v` is debug and `-q` is error: `slog` has no level above error
  that writes nothing, and error matches quiet in ADR-0013, where quiet leaves errors and data.
- **One source of the level.** `--log-level` together with `-v` or `-q` is a usage error that exits
  2. The conflict is found by whether a flag was set, not by its value, so `--log-level=info -v`
  conflicts. An unknown level or format is a usage error too.
- **Usage errors.** They are the internal usage error type of `internal/cobra`, not
  `spec.UsageError`: that type is for a run function that needs a prompt, and its text reads
  "requires interactive input".
- **Setup.** `logging.Setup(opts)` returns a `Result` and an error: it builds the logger, calls
  `slog.SetDefault` and returns the logger with the redacting writer beneath it. The hook stores
  both in the context (ADR-0016), where `cli.LoggerFrom(ctx)` and `cli.LogWriterFrom(ctx)` read
  them.
- **Two redaction layers, one redactor.** Logging always installs a redacting `slog.Handler` and a
  redacting `io.Writer` beneath it, built from one shared redactor. No option or constructor skips
  either: the writer is the backstop for text that reaches the destination without passing the
  handler.
- **Baseline.** Always on: the header names Authorization, Proxy-Authorization, Cookie and
  Set-Cookie, and the values `Bearer <token>` and `Basic <token>`. `WithLogging()` alone is valid.
- **Additions.** A tool adds with `cli.WithRedaction(logging.Redaction{Prefixes, Headers})` and
  cannot remove from the baseline. `WithRedaction` without `WithLogging` is an invalid command tree.
- **Matching.** A prefix followed by a run of `[A-Za-z0-9_+/=.~-]` becomes the prefix followed by
  `[REDACTED]`; a bare prefix is left alone. A header matches by attribute key, in any letter case
  and at any group depth, or as `Name: value` in text. Each line is read by its own shape, whatever the
  log format: a whole line that is valid JSON (it starts with `{` or `[`) is read as JSON, where a
  header value ends at the closing unescaped quote, and the value of a header key that is an array,
  an object, a number, a boolean or null becomes the string `"[REDACTED]"`, so the line stays valid
  JSON; any other line is
  read as text, where the value runs to end of line and the quoted form `"Name": value` also
  matches. A line written in pieces, past the 64 KiB cap or by a flush, takes its shape from the
  bytes held when its first piece is written: JSON when they are a whole JSON line or start with
  `{` and are the start of a JSON document, every token well formed; text otherwise, so a line
  that only starts with `{`, such as a Go `%+v` dump, never has a header value end at a quote.
- **Shape, not format, selects the pattern.** The writer also carries raw and subprocess output,
  which is not in the format the logger was given.
- **Writer scope.** Only the log writer is redacted, plus that same writer exposed by
  `cli.LogWriterFrom(ctx)` for raw or subprocess output. `Env.Stderr`, error rendering, `inv.Info`
  and the other message helpers, and the cobra error stream are untouched: wrapping `Env.Stderr`
  would hide the `*os.File` the editor launcher needs (ADR-0017) and change the golden-pinned error
  output and ADR-0013's streams.
- **Buffering.** The writer is line-buffered with a 64 KiB cap and a 256-byte trailing window kept
  on overflow. A final line without a newline is held until `Flush` or `Close`, or until the run ends, when the
  App flushes it on the run's own goroutine. A run abandoned on a second interrupt flushes when
  that run ends, never earlier, so the flush cannot race a write.
- **One default per process.** The default is process-wide, so executions that overlap in one
  process, of one App or of two, share the logger installed last, and a record written through the
  default may go to the stderr of another execution. Code that needs the logger of its own
  execution reads it with `cli.LoggerFrom`. A per-execution default is not possible: `slog` holds a
  single one.
- **Writer.** The records go to the stderr of the App's resolved `Env`, not to `os.Stderr`, so a
  test that substitutes the `Env` reads them.
- **Format.** `logging.Options.Format` is `spec.Format`, the type `--output` resolves to, so there is
  one notion of text and JSON.
- **Hook order.** The hook is chained after the `WithConfig` hook and before every `WithPreRun`
  hook. Loading the configuration therefore cannot log, and a configuration file cannot set the
  level, which only the command line does.

## Considered options

- **A counter flag, `-vv` for debug.** `-v` is a boolean of `WithVerbose` that the output mode also
  reads, and a counter would change its type for every tool that takes the option.
- **`-q` as warn.** Quiet means errors and data (ADR-0013), and warn would let records through that
  quiet suppresses everywhere else.
- **A return-only `Setup`, with no `SetDefault`.** Libraries a tool imports log through the `slog`
  default, and a logger only the hook's context carries would never reach them.
- **Letting `-q --log-level debug` through, by last flag wins or by a precedence.** Command line
  order is not visible after parsing and a precedence would have to be learnt, so setting a level
  twice is refused.
- **`WithLogging(opts...)` carrying the redaction.** Redaction is a separate `WithRedaction`, so
  `WithLogging()` stays argument-free and valid alone.
- **A tool-only pattern set with no baseline.** A tool that forgot to list `Authorization` or
  `Bearer` would log tokens.
- **Wrapping `Env.Stderr`.** It would break the editor launcher, which needs the raw `*os.File`
  (ADR-0017), and change the golden-pinned error output and the streams of ADR-0013.

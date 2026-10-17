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
- **Setup.** `logging.Setup` builds the logger, calls `slog.SetDefault` and returns it. The hook
  also stores the logger in the context (ADR-0016), where `cli.LoggerFrom(ctx)` reads it.
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

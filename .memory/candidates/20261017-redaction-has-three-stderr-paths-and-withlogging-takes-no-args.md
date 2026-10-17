---
about: "cli/core writes to env.Stderr by three routes (slog via Setup, errrender, inv messages) and ADR-0018 says nothing on redaction; WithLogging() takes no arguments, options-with-arguments precedent is WithConfig/WithExitCode/WithErrorRenderer"
saw:
  - docs/adr/0018-logging-is-an-opt-in-app-option.md
  - docs/adr/0013-stdout-carries-data-stderr-carries-messages.md
  - source/cli/core/logging.go
  - source/cli/core/logging/logging.go
  - source/cli/core/app.go
  - source/cli/core/execute.go
---

Context: planning issue #41 (secret redaction for the log handler and writer).

- ADR-0018 has no word on redaction, a writer wrapper, or configuring Setup beyond Level/Format/Writer.
  Its Writer decision (line 33) fixes the writer as the resolved Env's stderr. `logging.Options` is
  {Level, Format, Writer} (`logging/logging.go:16-23`); `Setup` (`:43`) builds the text/JSON handler on
  that writer and calls `slog.SetDefault` (`:59`). The hook builds Options in `loggingFlags.options`
  (`logging.go:92-93`) and sets only Level, Format, Writer: a tool has no way to inject anything.
- `WithLogging()` takes no arguments and only sets `s.logging = true` (`logging.go:40-42`).
  Options with arguments exist: `WithConfig[T](defaults)` (`config.go:51`), `WithExitCode`
  (`app.go:107`), `WithExitCodeAs` (`:120`), `WithErrorRenderer` (`:136`), `WithBuildInfo` (`:94`). All
  store into `settings` (`app.go:41`), read-only after `New`.
- Stderr paths outside slog: `errrender.Render(env.Stderr, ...)` at `execute.go:214` and `:266`
  (also `app.go:202`), `os.Stderr` in `execute.go:60` (failed Getwd), `inv.Info/Success/Warning/Error/Detail`
  (ADR-0013, `Env.Stderr`), `config edit` prompt (`command/config/edit.go:126`) and the editor receiving
  the raw *os.File (`edit.go:204-211`, must stay *os.File), cobra `SetErr(env.Stderr)` (`internal/cobra/cobra.go:107`).
  A redacting writer inside `logging.Setup` covers only the slog route; wrapping `Env.Stderr`
  generally would break the `*os.File` requirement of the editor launcher.
- Third-party policy: no blanket rule. ADR-0009 holds `paths` to stdlib + spec; ADR-0010 accepted
  `golang.org/x/mod` as the first third-party require of cli/spec. cli/core already requires cobra, pflag
  (`source/cli/core/go.mod`). Stdlib is enough for redaction.
- Release state: `docs/release-notes/` holds only `commons/`; no `cli/core` notes, so cli/core is not in
  strict mode. ADR-0016:50 and ADR-0017:18 say "cli/core has no release", so API changes break no consumer.
  (Tags not checkable: the checkout is not a git repo.)

Verdict on `20261017-with-logging-hook-usage-errors-and-global-default.md`: still-true. Re-read
`logging.go:74-87` (hook returns `&cobra.UsageError`), `logging/logging.go:59` (SetDefault), and the
`WithLogging` doc at `logging.go:32-35` (overlap caveat). `loggingProblem` is at `logging.go:183`.

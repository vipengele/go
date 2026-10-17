---
about: "cli/core writes to env.Stderr by several routes and only the log writer is redacted; cli.WithRedaction is separate from the argument-free WithLogging; redaction needs no third-party dependency"
saw:
  - docs/adr/0018-logging-is-an-opt-in-app-option.md
  - docs/adr/0013-stdout-carries-data-stderr-carries-messages.md
  - source/cli/core/logging.go
  - source/cli/core/logging/logging.go
  - source/cli/core/execute.go
---

- Stderr paths outside slog: `errrender.Render(env.Stderr, ...)` and `os.Stderr` for a failed Getwd in
  `execute.go`, `inv.Info/Success/Warning/Error/Detail` (ADR-0013, `Env.Stderr`), the `config edit`
  prompt and the editor, which receives the raw `*os.File` (`command/config/edit.go`, ADR-0017), and
  cobra `SetErr(env.Stderr)` (`internal/cobra/cobra.go`). ADR-0018 records that only the log writer,
  also exposed by `cli.LogWriterFrom`, is redacted: wrapping `Env.Stderr` would hide the `*os.File` the
  editor needs and change the golden-pinned error output.
- `WithLogging()` takes no arguments; a tool adds secrets with the separate `cli.WithRedaction`, which
  without `WithLogging` is an invalid command tree (`loggingProblem` in `logging.go`). The baseline
  (Authorization, Proxy-Authorization, Cookie, Set-Cookie, `Bearer `, `Basic `) is always on.
- `logging.Setup` returns `Result{Logger, Writer}` and builds one redactor for the handler wrapper and
  the writer; no option skips either.
- Redaction is stdlib only. `docs/release-notes/` holds no `cli/core` notes, so API changes there break
  no consumer (ADR-0016 and ADR-0017 say cli/core has no release).

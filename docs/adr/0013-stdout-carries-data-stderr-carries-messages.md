# stdout carries data, stderr carries messages

Status: accepted.

A command that writes a warning to the standard output in a `--json` run corrupts the JSON document
a script is reading. Every line a command writes goes to one of two streams: the standard output
carries the **Data** the command exists to produce, and the standard error carries the **Messages**
about the run. `spec.Invocation` has one method per kind, so a command never chooses a stream by
hand. The precedent is gt's quiet mode, which silences progress and keeps results and errors.

## The rule

- **Messages.** `Info`, `Success`, `Warning` and `Error` take `(format, args...)` and write one
  styled line to `Env.Stderr`, with a newline. The style is the `Styler` for `spec.Stderr`, so
  colour follows the stderr decision of the output mode. `Detail` writes a dim line with no glyph,
  and only when the run is verbose.
- **Data.** `Data(format, args...) error` writes exactly the formatted text to `Env.Stdout`:
  unstyled, with no newline added, and never suppressed. An encoder may write to `Env.Stdout`
  directly; that is the same data path.
- **Quiet.** Quiet silences everything except errors and data. `Info`, `Success`, `Warning` and
  `Detail` are dropped; `Error` and `Data` always write.
- **Write errors.** `Data` returns the write error, because a lost result is a failure the command
  must see. The message methods return nothing and ignore the write error: a message that cannot be
  shown has no better place to be reported.
- **`--json`.** It leaves messages on stderr unchanged and does not imply quiet.
- **`inv.Error`.** It is for an error a command shows itself: a non-fatal one, or one shown before
  returning `spec.Reported(err)`. An error a command returns is still printed by the App's
  renderer.

## Two error shapes

Two error shapes reach stderr by design. `inv.Error` prints `✗ msg`, with colour where the
stderr decision allows it. The App's renderer prints `<tool>: msg` as plain text, as a layer tree
([ADR-0011](0011-errors-render-as-a-layer-tree-with-typed-renderers.md)). Quiet affects neither: an
error is never dropped.

## Considered options

- **`--json` implies quiet.** It couples two flags: a user who wants JSON and also the warnings has
  no way to ask for them, and quiet gains a second meaning. Messages are already off the data
  stream, so the JSON document is intact without it.
- **Quiet keeps warnings.** A warning is a message, and the user asked for errors only. A tool that
  needs a warning to be seen in quiet mode has an error.
- **Lint enforcement, such as `forbidigo` on `os.Stdout`.** Commands already never touch
  `os.Stdout`: they write to the `Env`, which a test replaces. A rule would guard a path nobody
  takes and add a configuration to every project.
- **A separate `spec.Messages` type or a `spec/ui` package.** Each call site gains one hop to reach
  the writer, and the Invocation already holds the streams, the styler and the output mode the
  methods read.

## Consequences

- **A command has one way to speak.** A warning cannot reach stdout, so a `--json` run stays
  parseable whatever the command reports along the way.
- **Quiet is errors and data.** A command that wants something shown in every mode writes it with
  `Data` or reports it as an error.
- **A dropped message is lost.** The methods return nothing, so a closed stderr goes unnoticed.
- A change to `cli/spec` selects its dependents in CI (ADR-0006).

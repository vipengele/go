---
about: "cli/core flushes the redacting log writer from executeArgsEnded on the run's own goroutine, never from ExecuteArgs returning, because a run abandoned on a second interrupt keeps writing"
saw:
  - source/cli/core/execute.go
  - source/cli/core/app.go
  - source/cli/core/logging.go
  - source/cli/core/logging_test.go
---

- `App.ExecuteArgs` passes `logFlags.flush` as the `ended` callback of `executeArgsEnded`
  (`execute.go`), which `runThenEnd` defers around `cobra.Run` on the goroutine that runs the command.
  `executeArgsCtx` keeps its old signature by delegating with a nil `ended`, because `export_test.go`
  calls it.
- A `defer logFlags.flush()` in `ExecuteArgs` is wrong: after a second interrupt `executeArgsCtx` returns
  while the abandoned run still writes, and `RedactingWriter.Flush` mid-run emits a half-written header
  name or prefix (`Author`), so the rest (`ization: secret`) arrives later and matches nothing.
  `TestAbandonedRunIsFlushedWhenItEnds` (`logging_test.go`) pins it.
- Consequence: an abandoned run's last unterminated line is flushed when that run ends, after
  `ExecuteArgs` has returned; if the process exits first, that line is lost. Closing the writer instead
  was rejected: it would silence the process-wide `slog.Default` after every normal run.

---
about: "logging/logtest.Recorder is a standalone slog.Handler for tests that never goes through logging.Setup and never redacts; redaction is asserted through Setup's Options.Writer, which is how it stays consistent with ADR-0018's 'no option or constructor skips either layer'"
saw:
  - source/cli/core/logging/logtest/recorder.go
  - source/cli/core/logging/logging.go
  - docs/adr/0018-logging-is-an-opt-in-app-option.md
---

Verified in `source/cli/core/logging/logtest/recorder.go` and `logging.go`:

- `logtest.New()` returns a `*Recorder` that is an `slog.Handler` on its own store. It is not
  built by `logging.Setup`, and `Setup` has no handler-injection seam and calls `slog.SetDefault`,
  so a helper wrapping it would make parallel tests share process-global state and would return
  decoded text, not records.
- The package comment of `logtest` states the ADR-0018 reading: ADR-0018 governs what `Setup`
  installs (both redaction layers, always). A test that wants to prove redaction runs `Setup` with
  a buffer as `Options.Writer` and reads what reaches it, as `logging/example_test.go` does; the
  Recorder stores every attribute exactly as the code under test wrote it.
- Consequence for tests of a command built on `cli/core`: assert on levels, messages and
  attributes with a Recorder; assert that a secret is redacted only through the `Setup` writer.

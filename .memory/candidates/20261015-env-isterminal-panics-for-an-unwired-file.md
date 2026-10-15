---
about: "Env.IsTerminal with a nil Terminal panics for a file-like stream and answers false for a buffer; cli/core's executeArgs fills a nil Terminal on its resolved copy, so a real *os.File works there"
saw:
  - source/cli/spec/stream.go
  - source/cli/core/execute.go
  - source/cli/spec/terminal/terminal.go
  - source/cli/core/output.go
  - source/cli/spec/.golangci.yml
---

- `(*spec.Env).IsTerminal` (`source/cli/spec/stream.go`) asserts `interface{ Fd() uintptr }` on the stream, because the `cli/spec` root must not import `os`. With a nil `Env.Terminal`, a stream without `Fd()` (a buffer, a nil stream) is false and a stream with `Fd()` panics: `false` for a real file would be a guess.
- `executeArgs` (`source/cli/core/execute.go`) fills a nil `Terminal` on its resolved copy with `terminal.Detect` over the resolved streams, so `ExecuteArgs(&spec.Env{Stdout: os.Stdout}, ...)` does not panic; a supplied `Terminal` is kept and the caller's `Env` is not mutated. A code path that builds an Env with a real file and reaches `colorForbidden` (`source/cli/core/output.go`) without going through `executeArgs` would panic.
- `golang.org/x/term` is imported only by `source/cli/spec/terminal/terminal.go`; depguard rules in `source/cli/spec/.golangci.yml` and `source/cli/core/.golangci.yml` deny it elsewhere. Module-graph pruning keeps `x/term` and `x/sys` out of the `go.sum` of a module that does not import `cli/spec/terminal` (`source/update` has no `go.sum`).

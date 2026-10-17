---
about: "a run function cannot return a usage error (exit 2): UsageError is private to cli/core/internal/cobra and spec.Coded refuses code 2, so prompts need a new spec-level error kind"
saw:
  - source/cli/core/exitcodes.go
  - source/cli/core/execute.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/internal/errrender/render.go
  - source/cli/spec/exit.go
  - source/cli/spec/env.go
  - source/cli/spec/terminal/terminal.go
---

Found while planning issue #33 (Confirm/Prompt).

- Exit 2 is produced only by `errors.As(err, *cobra.UsageError)` (`core/execute.go:276-278`, rule 4).
  `UsageError` lives in `core/internal/cobra/cobra.go:26`, built only by the adapter for flag/arg parse
  failures (`cobra.go:101,179,190,229`). Code outside cli/core, including cli/spec, cannot import it.
- `spec.Coded(2, err)` does not help: `registrableExitCode` excludes 0, 2 and 130 (`core/exitcodes.go:21`),
  and `exitCode` ignores a coded value that fails it (`core/execute.go` exitCode func), falling to rule 4/5 -> exit 1.
- There is no typed error with a hint/remedy field in cli/spec or cli/core. `Hint` exists only in
  `source/config` (Schema.Removed), as text inside a fmt.Errorf.
- Renderer: `errrender` treats `*cobra.UsageError` as a flat leaf (`render.go:107`);
  `WithErrorRenderer[E]` is per-App and tried by direct type assertion on each node.
- Adding a new error kind that should exit 2 needs a change in exitCode (`core/execute.go`), the doc
  list in `core/doc.go:65`, execute.go docs (rules list), and likely ADR-0010 amendment; a spec-level
  type (spec/exit.go style, no I/O) is the only place update/daemon can reach (ADR-0010 "Where the types live").
- Terminal detection: `Env.IsTerminal(spec.Stdin)` (`spec/env.go`, `spec/stream.go`); nil `Env.Terminal` panics for
  a real file, so only an Env from `ExecuteArgs`/`Execute` (which fills it with `terminal.Detect`,
  `core/execute.go` doc) is safe. x/term is already a direct dep of cli/spec (`spec/go.mod:8`) and
  depguard confines it to `cli/spec/terminal` in both `spec/.golangci.yml:16` and `core/.golangci.yml:16`;
  no new dependency is needed. Root package `spec` imports no `os` (`spec/AGENTS.md`).
- Messages go through `inv.Info/...` to Env.Stderr and ignore `--json`; `Data` is the only stdout path
  (ADR-0013, `spec/message.go`). No ADR mentions prompts, --yes or --no-input.

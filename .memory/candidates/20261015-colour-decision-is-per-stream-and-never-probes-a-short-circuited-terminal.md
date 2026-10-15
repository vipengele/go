---
about: "colour is decided per stream by spec.ColorAllowed, whose IsTerminal call is the last rule only because a nil Env.Terminal panics for file-like streams; zero Output allows colour, Styler text is identical modulo SGR"
saw:
  - source/cli/spec/colour.go
  - source/cli/spec/output.go
  - source/cli/spec/style.go
  - source/cli/spec/stream.go
  - source/cli/core/output.go
  - source/cli/core/execute.go
  - docs/adr/0011-errors-render-as-a-layer-tree-with-typed-renderers.md
---

Re-read after the colour-policy change; every pointer below was checked against the current code.

- **Precedence is first-match, in `spec.ColorAllowed` (`source/cli/spec/colour.go:16-35`):** `--no-color` off (`:17`); `NO_COLOR` or `<EnvPrefix>_NO_COLOR` non-empty off (`:20-27`); `FORCE_COLOR` non-empty and not "0" on, beating both a non-terminal stream and `TERM=dumb` (`:28-30`); `TERM=dumb` off (`:31`); else `env.IsTerminal(s)` (`:34`). An empty variable counts as unset (`envValue`, `:38-44`).
- **`IsTerminal` must stay the last rule.** `(*Env).IsTerminal` with a nil `Env.Terminal` panics for any stream with an `Fd() uintptr` method (`source/cli/spec/stream.go:59-62`), because `cli/spec` imports no `os` and cannot answer for a file. Calling it up front (for example to compute a local `isTTY` before the checks) would panic for `NO_COLOR=1` or `FORCE_COLOR=1` on a file stream whenever the Env was not built by cli/core. `ExecuteArgs` only avoids this by filling a nil `Terminal` on its resolved copy (`source/cli/core/execute.go:143-144`). Doc comment at `colour.go:13-14` states the "only when rule 5 decides" contract.
- **Per-stream state:** `spec.Output` keeps `noColorStdout` and `noColorStderr` (`source/cli/spec/output.go:14-15`); `Output.Color(s)` is true for Stdout/Stderr unless forbidden and false for Stdin or any other stream (`output.go:57-66`). The core resolver calls `ColorAllowed` once for each of Stdout and Stderr with the same `--no-color` (`source/cli/core/output.go:27-33`), so redirecting stdout leaves colour on a stderr that is a terminal.
- **The zero `Output` allows colour on both streams** (the fields are negative, `no*`; `output.go:3-9`). A hand-built `spec.Output{}` in a test therefore yields coloured `Styler` output, unlike a resolved Output, whose result depends on the Env. Use `NewOutput(OutputOptions{NoColorStdout: true, ...})` for colourless tests. The zero `Styler` (not obtained through `Output.Styler`) has colour off (`source/cli/spec/style.go:33-37`).
- **Styler text is identical modulo SGR:** each styled run is `ESC[<code>m` + text + `ESC[0m`, so stripping SGR yields exactly the colour-off return value (`style.go:28-32, 66-73`). Empty text is returned as is, so no `ESC[1mESC[0m` surrounds nothing (`style.go:69`). The `Glyph*` constants are plain UTF-8 and the same with colour on or off; `TERM=dumb` changes only colour and there is no ASCII fallback (`style.go:3-5`), matching ADR-0011 ("always UTF-8 with no colour and no ASCII fallback", `docs/adr/0011-errors-render-as-a-layer-tree-with-typed-renderers.md:21`). Note the glyph prefix is inside the coloured run for Info/Success/Warning/Error (`style.go:55-64`).
- **Package-level `Execute`/`ExecuteArgs` have no identity or flag**, so they read only `NO_COLOR`, never a prefixed variable (`ColorAllowed` skips a nil identity, `colour.go:23`; `source/cli/core/AGENTS.md` gotchas).
- Env is read only through `Env.LookupEnv`, so tests must use a fake `LookupEnv`, not `t.Setenv` (`colour.go:42`).

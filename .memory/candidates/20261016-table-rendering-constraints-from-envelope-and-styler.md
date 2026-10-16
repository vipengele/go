---
about: "a table helper must fit WriteJSON's object-only envelope, the Styler's SGR-only colour contract and spec's no-I/O root package; no table/list/width code or third-party colour dep exists"
saw:
  - docs/adr/0014-json-output-envelope.md
  - docs/adr/0013-stdout-carries-data-stderr-carries-messages.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - source/cli/spec/AGENTS.md
  - source/cli/spec/style.go
  - source/cli/spec/.golangci.yml
  - source/cli/spec/go.mod
  - source/cli/core/go.mod
  - .lydite/components.yml
---

Found while planning issue vipengele/go#32 (table and list rendering). `agtk memory search` had no note on the cli output layer; this is read from ADRs and code.

- `spec.WriteJSON(w, schema, v)` accepts only a value that marshals to a JSON object, so rows cannot be the top-level value; a table in JSON mode needs a named key (`{"schema":1,"rows":[...]}`). Contract: ADR-0014 "The envelope". Flat, `schema` first, adding a key keeps `schema`, renaming/removing/retyping increments it.
- `speccheck.AssertJSONKeys` collapses array elements to `[]`, so per-row keys are pinned as dotted paths under the array key. It is a test helper, not a runtime requirement; a command pins its own payload, so a generic table helper has nothing of its own to pin.
- A command reads `inv.Output.Format()`, never a flag handle (ADR-0014). Data goes to `inv.Env.Stdout`, messages to stderr (ADR-0013); `Data` is never suppressed by quiet.
- Colour: `Output.Styler(spec.Stdout)` is the only abstraction (`source/cli/spec/style.go`: Bold, Dim, Info/Success/Warning, glyphs). Colour-on text equals colour-off text plus SGR, so a table must pad on the visible width of the plain text. Stdout colour is decided per stream, independent of stderr.
- `grep -rniE 'lipgloss|fatih|termenv|tabwriter'` over docs/ and source/cli found nothing: no decision either way on a third-party colour or table library. Existing deps: cobra+pflag (core only), `golang.org/x/term` (spec, confined to `spec/terminal` by depguard), `x/mod`, `x/sys`. ADR-0009 rejected `adrg/xdg` as a third-party dep for a few lines of code, which is the nearest precedent.
- Root package `spec` does no I/O and imports no `os` (`source/cli/spec/AGENTS.md`); `gochecknoglobals` is on in spec; no testify/go-cmp. A table package that only writes to an `io.Writer` fits in `cli/spec` as a subpackage; `cli/core` is the cobra adapter and app shell (ADR-0003, ADR-0005).
- Module split: `cli/spec` is the contract project that `update` and `daemon` import; `update` does not import `cli/core` (ADR-0005). A helper placed in `cli/core` would not be reachable from `update` commands.
- A change to `cli/spec` selects its dependents in CI (ADR-0006). `.lydite/components.yml` already has `cli-spec` and `cli-core` components; coverage thresholds live in the external reusable workflow, not this repo.

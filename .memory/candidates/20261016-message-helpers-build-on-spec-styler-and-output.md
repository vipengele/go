---
about: "issue #30 message helpers must extend the existing spec.Styler/Output/Env stream model, not add a new output abstraction"
saw:
  - source/cli/spec/style.go
  - source/cli/spec/output.go
  - source/cli/spec/env.go
  - source/cli/core/output.go
  - source/cli/core/.golangci.yml
  - source/cli/spec/AGENTS.md
  - .lydite/components.yml
---

Pre-planning read for "message helpers and the stdout/stderr rule".

- No IOStreams-like object exists. Commands get I/O only via `inv.Env` (Stdout/Stderr/Stdin io.Writer/Reader, nil = discard, `Resolved()`), `source/cli/spec/env.go`. Presentation comes from `inv.Output` (`spec.Output`: Verbose/Quiet/JSON/Color(stream)), resolved once per run in `source/cli/core/output.go` (verbose+quiet together = usage error).
- `spec.Styler` (`source/cli/spec/style.go`) already returns glyph+coloured strings for Info/Success/Warning/Error, per stream via `Output.Styler(stream)`. Its doc says "message helpers extend it with further methods". It writes nothing; the writing/quiet/stream-routing helpers are the missing piece.
- Root `spec` performs no I/O (no `os` import) per `source/cli/spec/AGENTS.md`; gochecknoglobals on; depguard denies cobra outside `cli/core/internal/cobra` and x/term outside `cli/spec/terminal`. Colour needs no third-party lib (hand-rolled SGR in style.go).
- Lydite: components `cli-core` and `cli-spec` run `go test -race ./...` (`.lydite/components.yml`); a new package inside either needs no new component. No coverage threshold found in `.lydite/`.
- No ADR or doc states "stdout is data-only"; grep for reserved/diagnostic found nothing. The only stdout writer today is `versioncmd` (text or JSON to inv.Env.Stdout); errors go to stderr via errrender. The rule is new to this issue (candidate ADR). 

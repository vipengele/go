---
about: "message helpers are methods on spec.Invocation built on Styler/Output/Env; stdout is data only (ADR-0013)"
saw:
  - source/cli/spec/message.go
  - source/cli/spec/style.go
  - source/cli/spec/output.go
  - source/cli/spec/env.go
  - source/cli/core/output.go
  - source/cli/core/message_test.go
  - source/cli/spec/AGENTS.md
  - docs/adr/0013-stdout-carries-data-stderr-carries-messages.md
---

- No IOStreams-like object exists. Commands get I/O only via `inv.Env` (Stdout/Stderr/Stdin io.Writer/Reader, nil = discard, `Resolved()`), `source/cli/spec/env.go`. Presentation comes from `inv.Output` (`spec.Output`: Verbose/Quiet/JSON/Color(stream)), resolved once per run in `source/cli/core/output.go` (verbose+quiet together = usage error there, but `spec.NewOutput` accepts both).
- `Info`, `Success`, `Warning`, `Error`, `Detail` and `Data` are methods on `*spec.Invocation` in `source/cli/spec/message.go`, built on `Output.Styler(spec.Stderr)` (`source/cli/spec/style.go`), which writes nothing itself. They write through `inv.Env.Resolved()`, so a nil `Env` discards.
- Quiet drops Info/Success/Warning/Detail; Error and Data always write. `Detail` also requires verbose, and quiet wins when both are set. `--json` changes none of this. `docs/adr/0013-stdout-carries-data-stderr-carries-messages.md` records the rule, and `source/cli/core/message_test.go` pins that `--json` stdout is exactly the data document in a verbose and a quiet run.
- Root `spec` performs no I/O (no `os` import) per `source/cli/spec/AGENTS.md`; gochecknoglobals applies to tests too, so tables are built inside the test function. The `golangci-lint` v2 must be the version CI pins: an older binary built with an earlier Go refuses a module whose `go` directive is newer.

---
about: "cli output mode (--json) is spec.Output set by core's WithJSON inherited flag; ADR-0007/0013 fix JSON conventions; no --output flag, envelope or golden-keys helper exists"
saw:
  - source/cli/spec/output.go
  - source/cli/core/flags.go
  - source/cli/core/output.go
  - source/cli/core/render_golden_test.go
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - docs/adr/0009-the-app-owns-version-and-takes-hooks-as-options.md
  - docs/adr/0013-stdout-carries-data-stderr-carries-messages.md
---

Questions answered while scoping issue #31 (JSON envelope + contract test).

- Output mode is `spec.Output` (bool `json`, resolved per run), in `cli/spec` so update/daemon commands
  need no cli/core (ADR-0009). `core/flags.go` `WithJSON` declares `--json` as an Inherited root flag; without it
  `versioncmd` declares a local `--json`. `spec.Command.Validate` rejects a descendant redeclaring an
  inherited name, so a per-command `--json` collides with `WithJSON` (ADR-0009). `--output` exists only in
  `spec/flag_test.go` as sample data; no `WithOutput` anywhere (grep).
- Existing JSON precedent: `version --json` has `schema` int (not `version`), additive keys keep schema (ADR-0007).
  `version` as envelope key name would differ from that precedent and from buildinfo's own `version` string field.
- stdout=data, stderr=messages; `--json` does not imply quiet (ADR-0013). Writers write to `Env.Stdout`.
- Depguard: cobra only in core/internal/cobra; x/term only in spec/terminal (spec/.golangci.yml, core/.golangci.yml).
- Golden convention: `core/render_golden_test.go` `checkGolden`, `-update` flag, files `testdata/render/<name>.golden`
  (helper is local to that test, not shared). No testify/go-cmp in cli go.mod files.
- Strict mode: only docs/release-notes/commons/v0.1.0.md exists and `git ls-remote --tags origin` shows no
  `source/commons/v0.1.0`; commons is strict but has no siblings, cli/* have no notes so are in dev mode.

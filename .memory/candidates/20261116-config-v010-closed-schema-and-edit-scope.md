---
about: "cli/core loads config through Options.Load (Defaults, File, Env) while validate and edit use Defaults and File only, at a fixed path; cli/core requires config at a tag with no replace"
saw:
  - source/cli/core/config.go
  - source/cli/core/command/config/config.go
  - source/cli/core/go.mod
  - docs/adr/0017-the-config-command-group-is-an-opt-in-app-option.md
---

- cli/core: `Options.Load` runs `cfg.Load(Defaults, File, Env)` (command/config/config.go); `validate`
  and `edit` use `loadFile`, Defaults and File only (ADR-0017 "Scope of the checks"). The file path is
  fixed at `<Dirs.Config()>/config.yaml` (cli/core/config.go); there is no `--config` flag, by
  ADR-0017's design. `show` reads the context's result, the full stack.
- Doc drift: ADR-0017 says `edit` renames through `internal/atomicfile`; the code uses
  `state.Replace` (cli/state, edit.go).
- cli/core's go.mod requires `go.vipengele.org/config` at a tag with no `replace`: a change that
  needs both projects developed together adds a directory replace (ADR-0006) in one pull request.

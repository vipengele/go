---
about: "cli/core loads config through Options.Load (Defaults, user file, overlay, Env); validate drops Env and edit sees only the user file, at a fixed path; cli/core develops config through a directory replace"
saw:
  - source/cli/core/config.go
  - source/cli/core/command/config/config.go
  - source/cli/core/command/config/validate.go
  - source/cli/core/command/config/edit.go
  - source/cli/core/go.mod
  - docs/adr/0017-the-config-command-group-is-an-opt-in-app-option.md
---

- cli/core: `Options.Load` runs `cfg.Load(Defaults, File, overlay, Env)` (command/config/config.go),
  the overlay being `File(path).Allow(OverlayAllow...)` when `Options.overlay` resolves one.
  `validate` loads Defaults, the user file and the overlay without Env (validate.go); `edit` loads
  Defaults under its copy of the user file alone (edit.go). The user file path is fixed at
  `<Dirs.Config()>/config.yaml` (cli/core/config.go); there is no `--config` flag, by ADR-0017's
  design. `show` reads the context's result, the full stack.
- `edit` renames through `state.Replace` (go.vipengele.org/cli/state, edit.go), as ADR-0017 says.
- cli/core's go.mod requires `go.vipengele.org/config v0.1.0` with `replace => ../../config` while
  it uses the unreleased `FileProvider.Allow`: releasing cli/core first needs config tagged at a
  version carrying `Allow`, then the replace dropped (ADR-0006).

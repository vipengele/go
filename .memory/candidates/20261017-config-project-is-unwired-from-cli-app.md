---
about: "source/config is a standalone project nothing imports; cli/core's App has no config loading, no skip-config annotation, and the repo has no atomic-write, editor or y/n prompt helper"
saw:
  - source/config/doc.go
  - source/config/config.go
  - source/config/go.mod
  - source/cli/core/app.go
  - source/cli/core/hooks.go
  - source/cli/spec/command.go
  - source/cli/spec/env.go
  - docs/adr/0012-configuration-is-a-provider-based-project.md
---

Checked for a "standard config command" (issue #37) groundwork.

- `go.vipengele.org/config` (source/config) requires only go.yaml.in/yaml/v3 (`source/config/go.mod`) and no
  other project imports it: `grep -rn "vipengele.org/config" --include=go.mod --include=*.go source` outside
  source/config -> 0 hits. ADR-0012 says the adapter from an Invocation to the `config.Flags(map[string]string)`
  map "belongs to the project that knows both sides"; it does not exist yet.
- `cli.App` (`source/cli/core/app.go`) has no config option. Only generic `WithPreRun`/`WithPostRun` hooks
  (`hooks.go`) that receive `spec.CommandInfo`, whose annotations (`spec.Command.Annotations`,
  `command.go:36`) are read per command, not inherited. The only annotation key is `spec.SkipUpdateCheck`.
  No skip-config key exists.
- `config.Result[T]` already records per-key provenance: `Sources() []config.Source{Key, Value, Origin}`
  (`config.go:41`, `sources.go`), origin "default", "config.yaml:12", "env APP_X", "flag --x".
- No os.Rename, fsync, editor ($VISUAL/$EDITOR) or y/n prompt code anywhere under source/
  (`grep -rniE 'os.Rename|\.Sync\(|EDITOR|VISUAL|ReadString|bufio'` -> 0 non-test hits). The only writes:
  `config.Seed` (O_EXCL, 0600, `seed.go:52`), `speccheck` golden (`speccheck.go:138`), `paths.Ensure` chmod.
- cobra is confined to `source/cli/core/internal/cobra` by a depguard rule in `source/cli/core/.golangci.yml`
  (ADR-0005), so a "cobra constructor" cannot live outside it: commands are `*spec.Command` values.
- `paths.Dirs.Config()` returns a directory only; no config file name or --config/env override exists.

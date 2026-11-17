---
about: "config v0.1.0 rejects maps/pointers by schema walk alone (no ADR); every layer merge, Source, File strictness, Seed and edit scope assume one flat key per leaf"
saw:
  - source/config/schema.go
  - source/config/bind.go
  - source/config/file.go
  - source/config/seed.go
  - source/config/sources.go
  - source/config/env.go
  - source/cli/core/config.go
  - source/cli/core/command/config/config.go
  - docs/adr/0012-configuration-is-a-provider-based-project.md
  - docs/adr/0017-the-config-command-group-is-an-opt-in-app-option.md
---

Explored for issue vipengele/go#215 (maps, struct slices, extra file layer). Memory had no note on config.

- Map/pointer rejection is in schema.go walk (`case Pointer`, `case Map`, ~l.196-199); supportedValue allows only scalars/Duration and slices of them. No ADR (0012, 0017) mentions maps or pointers: unimplemented, not a decision.
- Everything is keyed by flat Field list: Entry{Key,Value|Items,Origin} (file.go), binder.origins[i] per field (bind.go), Source{Key,Value string,Origin} (sources.go), Seed walks schema.fields (seed.go renderDefaults), Env iterates schema.fields (env.go). No presence tracking: a key is set only if a provider emitted an Entry; unset vs zero is "no Entry".
- Merge is whole-value replace per key, slices included (config.go Load doc); cloneSlices exists to stop aliasing of defaults.
- File strictness is hand-walked over yaml.Node (file.go fileLoader.mapping/pair; groupKeys for prefixes), not KnownFields. Provider.Strict() is informational only; nothing in the repo reads it.
- Env names derived key->name (key.go envName); ADR-0012 rejects scanning because APP_A_B_C is ambiguous. A map key / slice index has no derivable var.
- cli/core: Options.Load = cfg.Load(Defaults, File, Env) (command/config/config.go); validate/edit use loadFile = Defaults+File only (ADR-0017 "Scope of the checks"); path fixed at <Dirs.Config()>/config.yaml (cli/core/config.go), no --config flag by ADR design. show uses ctx result, full stack.
- Doc drift: ADR-0017 says edit renames via `internal/atomicfile`; code uses `state.Replace` (cli/state, edit.go).
- cli/core requires config v0.1.0 with no replace in go.mod: developing both needs a directory replace (ADR-0006) and a single PR.

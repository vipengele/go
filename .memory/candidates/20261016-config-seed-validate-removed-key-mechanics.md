---
about: "config's Seed, Validate and removed-key mechanics: yaml string tagging, annotated FieldError copies, and where Removed is consulted"
saw:
  - source/config/seed.go
  - source/config/validate.go
  - source/config/schema.go
  - source/config/file.go
  - source/config/flags.go
  - source/config/bind.go
---

Checked 2026-10-16 while landing issue #36.

- Seed tags every string scalar `!!str` (seed.go scalarNode). Without it the yaml.v3 encoder writes `yes`, `123`, `null` or `~` plain and the file loader reads a null as no value, so the seeded file would not read back to the defaults. The YAML module imported is `go.yaml.in/yaml/v3`, not `gopkg.in/yaml.v3`.
- Seed renders the whole document into a buffer before it creates the file (seed.go Seed, renderDefaults), so an unencodable value (a string with invalid UTF-8) leaves no file behind. The create is injected into `writeNew` so the remove-on-write-failure path is testable without a package-level variable (`gochecknoglobals` is on).
- Load never mutates the `*FieldError` a Validator returns. `annotate` (validate.go) builds copies carrying the key's value and origin and returns a `*validationError` whose text is the original error's text with each FieldError's text substituted, so a `fmt.Errorf("...: %w", fe)` wrapper keeps its prefix; `Unwrap() []error` lists the copies before the original so `errors.As` finds the annotated one and `errors.Is` still reaches the original chain.
- `Schema.Removed` is consulted at three sites, each after the live-key lookup fails: file.go `fileLoader.pair` (default branch, after the Lookup and group checks), flags.go `FlagsProvider.Load`, and bind.go `binder.bind`. Env never reports one: it looks up only names derived from schema keys. A removed key is matched exactly (relaxed spelling, not prefix), so a key below a removed key is still unknown.
- `NewSchema` accepts a RemovedKeys entry below a live value key (live `server`, removed `server.old`); the file loader reaches `server` as a value first, so that hint can never fire. Entries are rejected only when they equal a live key, a live key's group, or each other under `matchKey`.

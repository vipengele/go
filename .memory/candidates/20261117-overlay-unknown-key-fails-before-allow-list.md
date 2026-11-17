---
about: "a config overlay reports an unknown key before its allow-list, so \"not settable in <file>\" needs a schema key the allow-list omits; greet cannot produce it"
saw:
  - source/config/file.go
  - source/cli/core/config.go
  - source/examples/greet/app.go
  - docs/guide/consumer.md
---

- `fileLoader.pair` (source/config/file.go) looks the key up in the schema first and fails with
  `unknown key %q` (or `was removed`) when it is absent; only a key the schema has reaches
  `s.settable`, which fails with `key %q is not settable in <file>`.
- greet's configuration has one key, `greeting`, and its overlay allows exactly that key
  (`cli.WithConfigOverlay(projectFile, "greeting")` in source/examples/greet/app.go), so no greet
  run can show the "not settable" error. The guide's overlay subsection (docs/guide/consumer.md,
  "A project overlay") shows the real `project.yaml:2: unknown key "colour"` error and describes the
  "not settable" case in prose.
- `WithConfigOverlay` (source/cli/core/config.go:102) uses the resolved path as given: a relative
  path is relative to the process's working directory, not `spec.Env.WorkDir`. An empty path or one
  that names no file adds no layer.

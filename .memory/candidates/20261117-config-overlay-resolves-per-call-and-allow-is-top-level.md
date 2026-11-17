---
about: "the config overlay resolver runs on every Options.overlay call, so show resolves it again after the hook's Load; File.Allow checks top-level Schema keys only"
saw:
  - source/cli/core/command/config/config.go
  - source/cli/core/command/config/show.go
  - source/cli/core/command/config/validate.go
  - source/config/file.go
---

- `Options.overlay` (command/config/config.go) calls `Options.Overlay` each time: `Load` (the
  pre-run hook), `OverlayPath`, `show` and `validate` each resolve it afresh. `show` names the layer
  from its own call, so a resolver that is not deterministic within one execution can name a path
  other than the one the hook loaded. An empty path stats as missing, so there is no separate
  empty-path branch.
- A non-ErrNotExist stat error fails as `config overlay <path>: ...`; a directory passes stat and
  fails when `FileProvider.Load` reads it.
- `FileProvider.Allow` (source/config/file.go) is enforced in `fileLoader.pair` only through
  `scope.settable`, which only the configuration's scope sets: keys inside collection elements and
  map keys are never checked, the collection's own key governs them.
- `WithConfigOverlay` without `WithConfig` is reported by `configProblem` (cli/core/config.go),
  joined with `loggingProblem` in `App.ExecuteArgs`, as "invalid command tree".

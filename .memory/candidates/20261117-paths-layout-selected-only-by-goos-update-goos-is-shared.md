---
about: "paths.Options.Layout chooses the layout apart from GOOS (ADR-0039); GOOS stays the target OS, and in update it also drives asset ext, .exe name and container detection, so faking GOOS for XDG is wrong"
saw:
  - source/cli/spec/paths/layout.go
  - source/cli/spec/paths/paths.go
  - source/cli/core/app.go
  - source/cli/core/config.go
  - source/update/config.go
  - docs/adr/0039-paths-layout-is-independent-of-goos.md
---
- `paths.Layout` (layout.go) is `LayoutDefault` (zero: pick by GOOS), `LayoutXDG`, `LayoutDarwin`,
  `LayoutWindows`, with `Valid()`. `resolver.layout` maps LayoutDefault to darwin/windows/xdg by
  `r.goos`, else uses the chosen layout. `Options.GOOS` still feeds `Dirs.goos`, which drives the
  Windows permission skip in `ensure.go`, so the layout never changes OS behaviour.
- `Resolve` rejects an invalid Layout right after the kinds check and before the
  `Options.Home`/`<PREFIX>_HOME` shortcut (paths.go, `opts.Layout.Valid()`), so a home root cannot
  hide a bad value.
- cli/core: `WithPathsLayout` (app.go) stores `settings.pathsLayout`; every directory the App
  resolves goes through `settings.pathsOptions(lookup)` (today only `configFor` in config.go). An
  invalid layout is `pathsProblem`, joined with loggingProblem/configProblem in `App.ExecuteArgs`
  -> "invalid command tree", exit 1. `settings.goos`/`userHomeDir` are a test-only seam set by
  `WithPathsPlatform` in export_test.go; there is no public GOOS option.
- cli/core `PathsLayoutFrom(ctx)` (config.go) reads a value the `layoutHook` pre-run hook stores; it is
  the first hook in `chainHooks`, so it runs with or without WithConfig and on SkipConfig commands.
  It is not an Invocation method because paths imports spec (cycle).
- update: `Config.PathsLayout` is forwarded to `paths.Options.Layout` only when StateDir is empty
  (config.go `resolve`); `checkPathsLayout` rejects an invalid value in `Validate`. `Config.GOOS`
  still picks DefaultExtFor, the ".exe" name (extract.go) and InContainer (linux only), which is
  why faking GOOS="linux" to get XDG was rejected.
- daemon passes a caller-built `paths.Options` (daemon/instance.go), so it gets `Layout` for free.

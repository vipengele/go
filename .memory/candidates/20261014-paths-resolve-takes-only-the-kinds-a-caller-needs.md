---
about: "paths.Resolve resolves only the requested kinds plus the parent a requested kind derives from, so a caller that needs only State resolves it from an absolute XDG_STATE_HOME with no home directory; paths.New resolves all five and still fails with ErrNoHomeDir when any kind needs a missing home"
saw:
  - source/cli/spec/paths/paths.go
  - source/cli/spec/paths/layout.go
  - source/cli/spec/paths/paths_test.go
  - source/update/config.go
  - source/update/config_test.go
---

- `Resolve(id, opts, kinds...)` (`source/cli/spec/paths/paths.go`) marks the requested kinds (`requested`; no kinds means all five, an invalid `Kind` is an error) and `layout.go` `resolveKinds` resolves only those. `New(id, opts)` is `Resolve(id, opts)`.
- A derived kind reads its parent through the `dir` callback, which honours the parent's explicit option: log from state on XDG, state and data from config on darwin. The parent is resolved to derive the child but is left `""` in the returned `Dirs` unless it was requested too. An explicit option on the child itself means the parent is never resolved.
- A kind not requested is never resolved, so its failure cannot fail the call; on XDG, config, cache and data fall back to `~`, which is why `New` still needs the home directory where `Resolve(..., State)` does not.
- The home directory is looked up at most once per call (`resolver.homeDir` caches the result and the error), and only when a requested kind or its parent has neither an explicit option, a home root (`Options.Home`, `<PREFIX>_HOME`), nor an absolute XDG variable (`LocalAppData`/`AppData` on Windows).
- `Dirs.Ensure` on an unresolved kind returns "no directory resolved" for it (`ensure.go`).
- `update` (`source/update/config.go`) calls `paths.Resolve(..., paths.State)`; `TestResolveDefaultStateDirNeedsNoHomeDirWhenTheStateDirectoryIsSet` covers a failing `UserHomeDir` with `XDG_STATE_HOME` set.

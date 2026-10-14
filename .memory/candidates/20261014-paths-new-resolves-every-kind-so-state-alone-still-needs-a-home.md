---
about: "paths.New resolves all five kinds at once, so on the XDG layout a caller that needs only State still fails with ErrNoHomeDir when XDG_STATE_HOME is set but no home directory resolves, because config, cache and data fall back to the home directory; only <PREFIX>_HOME, Options.Home or explicit per-kind options avoid the lookup"
saw:
  - source/cli/spec/paths/paths.go
  - source/cli/spec/paths/layout.go
  - source/update/config.go
  - source/update/config_test.go
---

- `New` (`source/cli/spec/paths/paths.go`) resolves every kind eagerly. `layout.go` `xdg()` computes config, cache and data from `~/.config`, `~/.cache` and `~/.local/share` unless their own `XDG_*_HOME` is absolute, and each of those calls `underHome`, which returns `ErrNoHomeDir` when `UserHomeDir` fails or returns "".
- `update` (`source/update/config.go`) calls `paths.New` only for `.State()`, so its tests inject a working `UserHomeDir` even when they set `XDG_STATE_HOME`; a user who sets only `XDG_STATE_HOME` in an environment with no resolvable home gets an error from `resolve`.
- The home directory is looked up at most once, and only when some kind has neither an explicit option, a home root, nor an absolute XDG variable (`resolver.homeDir`).

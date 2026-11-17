---
about: "paths picks its layout only from Options.GOOS (ADR-0009), and update.Config.GOOS also drives asset ext and .exe, so faking GOOS=linux for XDG would break update; cli/core passes no GOOS at all"
saw:
  - source/cli/spec/paths/layout.go
  - source/cli/spec/paths/paths.go
  - source/cli/core/config.go
  - source/update/config.go
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
---
Checked 2026-11-17 for GitHub issue 229 planning. No ADR mentions XDG-on-macOS as a choice: `grep -il xdg docs/adr` hits only 0009, 0010-version, 0015-table, 0021-completioninstall, 0023-daemon-lock (all incidental).

- Layout selector is `Options.GOOS` only (paths.go:65-66, layout.go:10-18 switch: darwin / windows / default xdg). ADR-0009 rejected build tags so tests select any layout via GOOS; no separate Layout field exists.
- cli/core calls `paths.Resolve(a.identity, paths.Options{LookupEnv: lookup}, paths.Config)` at core/config.go:132 with no GOOS, i.e. runtime.GOOS. It resolves only Config kind.
- update resolves State at update/config.go:553-557 passing `GOOS: r.GOOS`. `update.Config.GOOS` (config.go:168-170) is ALSO the target OS for asset ext (DefaultExtFor, config.go:535), ".exe" binary name (extract.go:90) and InContainer (config.go:574). So faking GOOS="linux" in update.Config changes which release asset is downloaded: a separate paths layout field is needed rather than reusing GOOS.
- daemon also calls paths.Resolve (daemon/instance.go:69) with a caller-supplied paths.Options, so a Layout field on paths.Options would reach daemon for free; update and cli/core each build Options themselves.
- paths lives in cli/spec (released v0.1.0), so adding a field is a cli/spec release before core/update can use it.
- InContainer returns false for any GOOS but linux (container.go:55-60): a faked GOOS also changes container detection.

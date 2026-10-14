---
about: "update.Config's zero value resolves to production through unexported resolve(), which validates first, never mutates the receiver, and derives the default StateDir by calling paths.New with the resolved GOOS, Getenv and UserHomeDir seams; a nil Source is the one field that cannot default (ErrNoSource, surfaced only by Validate)"
saw:
  - source/update/config.go
  - source/update/source.go
  - source/update/config_test.go
  - source/cli/spec/paths/paths.go
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
---

- `Config.resolve()` (`source/update/config.go`) returns a filled copy and fails on `Validate()` first; the receiver is never modified, and `Config` holds func fields so it is not comparable (tests compare non-func fields).
- The default `StateDir` is `paths.New(Identity, Options{GOOS, LookupEnv, UserHomeDir}).State()` (`go.vipengele.org/cli/spec/paths`), with `Config.Getenv` adapted to the `LookupEnv` shape (non-empty means set). An explicit `StateDir` skips the resolver and never looks up the home directory. The seams mean a test that substitutes them never reads the host's environment. The default `HTTPClient` is a fresh `&http.Client{}`, never `http.DefaultClient`.
- A nil `Source` cannot default, so `Validate()` reports `ErrNoSource` (joined with the `Identity` errors via `validate.Join`); `resolve` is the only other caller, so it surfaces nowhere else yet. `Source`/`Release`/`Asset` in `source.go` are a minimal shape with no default implementation.
- ADR-0003's `Config{Name, EnvPrefix, Version, Source, StateDir, Token}` sketch is not the shape in code: identity comes from the embedded `spec.Identity`. ADR-0009 reverses ADR-0003's clause that has `update` keep its own resolver.

---
about: "update.Config's zero value resolves to production through unexported resolve(), which validates first, never mutates the receiver, and derives the default StateDir through the UserConfigDir seam; a nil Source is the one field that cannot default (ErrNoSource, surfaced only by Validate)"
saw:
  - source/update/config.go
  - source/update/source.go
  - source/update/config_test.go
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
---

- `Config.resolve()` (`source/update/config.go`) returns a filled copy and fails on `Validate()` first; the receiver is never modified, and `Config` holds func fields so it is not comparable (tests compare non-func fields).
- The default `StateDir` is `<UserConfigDir()>/<Identity.DirName>`, computed through the `UserConfigDir` seam rather than `os.UserConfigDir`, so a test that substitutes the seams never reads the host's environment. The default `HTTPClient` is a fresh `&http.Client{}`, never `http.DefaultClient`.
- A nil `Source` cannot default, so `Validate()` reports `ErrNoSource` (joined with the `Identity` errors via `validate.Join`); `resolve` is the only other caller, so it surfaces nowhere else yet. `Source`/`Release`/`Asset` in `source.go` are a minimal shape with no default implementation.
- ADR-0003's `Config{Name, EnvPrefix, Version, Source, StateDir, Token}` sketch is not the shape in code: identity comes from the embedded `spec.Identity`.

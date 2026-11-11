---
about: "a tool built inside the update module cannot use goreleaser.Render's default LinkVars (cli/core/buildinfo): the -X flags then name a package the binary does not link and silently set nothing, so the release fixture renders with main.version/commit/date"
saw:
  - source/update/internal/releasetest/fixture.go
  - source/update/goreleaser/render.go
  - source/update/testdata/releasefixture/main.go
  - source/update/.golangci.yml
---

- `goreleaser.Render`'s default link variables are the string constants `DefaultVersionVar`/`DefaultCommitVar`/`DefaultDateVar` naming `go.vipengele.org/cli/core/buildinfo` (`source/update/goreleaser/render.go`). The linker ignores `-X` for a symbol that is not linked, so a binary that does not import buildinfo builds fine with its version left at the default.
- update may not import cli/core (depguard `core-stays-out` in `source/update/.golangci.yml`), so the release fixture `source/update/testdata/releasefixture/main.go` declares its own `version`, `commit`, `date`, and `releasetest.FixtureOptions` (`fixture.go`) passes `LinkVars{Version: "main.version", ...}`.
- The fixture's committed `.goreleaser.yaml` is the golden of `internal/releasetest/golden_test.go`; `go test ./internal/releasetest -update` rewrites it, and `.gitattributes` keeps it LF so the byte comparison holds on Windows.

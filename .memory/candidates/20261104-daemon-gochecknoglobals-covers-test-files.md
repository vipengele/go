---
about: "daemon's gochecknoglobals also flags package-level vars in _test.go files, so test tables are functions or locals"
saw:
  - source/daemon/.golangci.yml
  - source/daemon/service/state_test.go
  - source/daemon/service/state.go
---

`source/daemon/.golangci.yml` enables `gochecknoglobals` on top of `default: standard`, with no
exclusion for tests. With the CI-pinned golangci-lint (v2.14.0, `.github/workflows/ci-build.yml`)
a package-level `var allStates = []struct{...}` in `service/state_test.go` is reported as
"is a global variable (gochecknoglobals)". The shared test table is therefore a function,
`func allStates() []struct{...}` in `source/daemon/service/state_test.go`.

The same linter is why `State`'s names in `source/daemon/service/state.go` go through `switch`
statements in `String`, `MarshalText` and `UnmarshalText` rather than a name table or reverse
map, and why `Config` validation in `service/config.go` uses hand-written rune checks rather
than package-level regexps. Only sentinel errors are package-level vars.

`gocritic` is not in `default: standard`, so identical `switch` branches (each GOOS case of
`New` in `service/manager.go` returning the same error) pass lint.

---
about: "source/update CheckManaged builds its Nix and Homebrew prefix lists inside the function because the repo's gochecknoglobals lint rejects package-level slices"
saw:
  - source/update/managed.go
  - source/update/.golangci.yml
---

- `CheckManaged` in `source/update/managed.go` passes the Nix and Homebrew prefix slices to
  `detectPrefixes` from inside the function body. A package-level `var` of prefixes fails
  `golangci-lint` (`gochecknoglobals`), which is also the package's no-mutable-state convention.
- Prefix matching is `hasPathPrefix`: equal to the prefix, or the prefix followed by `/`. A bare
  `strings.HasPrefix` would treat `/opt/homebrewX` as Homebrew.
- The container detector in the same list ignores the path and returns `Config.InContainer()`, so
  the symlink-resolution failure fallback only affects Nix, Homebrew and tool-supplied detectors.
- The golangci-lint on PATH in the cloud image (v2.5.0, built with go1.25) cannot load a module
  whose `go` directive is 1.27.x; the version pinned in `.github/workflows/ci-build.yml` can.

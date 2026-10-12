---
about: "A module whose path is go.vipengele.org/<dir> in the repository directory source/<dir> resolves through the default proxy, GOPROXY=direct, the checksum database and pkg.go.dev once a source/<dir>/vX.Y.Z tag exists and the Worker serves <dir>"
saw:
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - tools/vanity-worker/src/index.ts
---

A throwaway module at `source/spike/probe` with `module go.vipengele.org/spike/probe`, tagged
`source/spike/probe/v0.0.1`, resolved with `go get` through proxy.golang.org and with `GOPROXY=direct`,
and sum.golang.org returned a signed record for it. The proxy's `.info` reported
`Origin.Subdir: source/spike/probe` and `Ref: refs/tags/source/spike/probe/v0.0.1`. pkg.go.dev
rendered the package with source links under `.../blob/source/spike/probe/v0.0.1/source/spike/probe/`;
its first request answered 404 until the module had been requested. Resolution depends on the
deployed Worker listing the directory (`/<dir>?go-get=1` carries the four-field `go-import` tag with
the subdirectory), and that list is the one of the commit its `tools/vanity-worker/vX.Y.Z` tag points at.
A module with `go 1.27.2` makes an older local toolchain switch to that release on `go get`.

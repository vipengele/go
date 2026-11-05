---
about: "A depguard pkg entry matches an import path by prefix and takes a glob such as /** literally, so a pkg ending in /** denies nothing"
saw:
  - source/daemon/.golangci.yml
  - source/update/.golangci.yml
---

- In golangci-lint v2's depguard, `deny: - pkg: X` matches every import path that starts with
  `X`; `*` and `**` in `pkg` are not globs. `pkg: "go.vipengele.org/cli/core/**"` therefore
  matches no real import, and an import of `go.vipengele.org/cli/core` passes lint. Verified by
  adding such an import to `source/daemon/daemoncmd` with the `/**` entry in place.
- `source/daemon/.golangci.yml` writes the entry as `go.vipengele.org/cli/core` and carries a
  comment saying so. `source/update/.golangci.yml` still lists `"go.vipengele.org/cli/core/**"`,
  so update's ban on cli/core is not enforced by lint.
- Globs belong in `files:` (`"!**/daemoncmd/**"` works there). An `allow:` entry ending in `$`
  is an exact match (`go.vipengele.org/daemon$` in source/daemon/.golangci.yml lets the root
  package's external tests import their own package under a `go.vipengele.org/` deny).

---
about: "module path includes source/<project>, tag is source/<project>/vX.Y.Z, and a v2+ adds /v2 after the directory while the tag keeps the prefix; mistakes surface only after the tag is pushed"
saw:
  - agentic/rules/module-path-matches-its-directory.md
  - docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md
  - CONTEXT.md
---

Kind: invariant / gotcha.

The proxy finds a nested module only from tag `source/<project>/vX.Y.Z` and then requires the module path to end in that directory. A mismatched path (e.g. `.../go/cli`, or a `go mod init` without the full path) builds locally and in CI but every `go get` fails, and only after the tag is pushed. A pushed tag cannot be moved, so a rename starts a new version history. A bare `vX.Y.Z` tag is not a release here. For v2+: module path `github.com/vipengele/go/source/<project>/v2`, tag `source/<project>/v2.0.0`.
`verify-sibling-deps` is the pre-tag guard for path/dir mismatch.
Why the odd `source/` in import paths: it is the price of "no code at repo root" (ADR-0001 rejected root-level modules and lockstep/one-module layouts; vipengele/typescript's single repo-wide tag does not work for Go nested modules).
Evidence: ADR-0001, rule file.

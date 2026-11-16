---
about: "a CI-checked consumer example that wires cli/core, config and update cannot sit in any existing project without a new sibling edge"
saw:
  - source/update/.golangci.yml
  - source/cli/core/go.mod
  - source/update/go.mod
  - source/update/testdata/releasefixture/main.go
  - .github/workflows/release-tool-selftest.yml
  - .lydite/components.yml
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
  - source/examples/greet/go.mod
---

Context: planning vipengele/go#67 "Consumer guide" (acceptance: every snippet compiles in a CI-checked example).

- cli/core requires cli/spec, cli/state, config (source/cli/core/go.mod) and NOT update.
- update requires cli/spec, cli/state, commons, filelock and NOT cli/core; depguard rule `core-stays-out` in source/update/.golangci.yml denies the import (ADR-0008).
- No project requires both, so an example showing App + config + update in one program needs a new project or a new cross-project edge that ADR-0008 forbids. The example is `source/examples/greet`: its own go.mod requiring each sibling at its tag with no replace, a go.work use line, a .lydite component and a .gt-repo.yaml gomod entry.
- Per-library `Example` functions already exist in each project (`example_test.go` in cli/core, cli/spec, config, update, ...), so per-snippet compilation is covered within a single project.
- Precedent for a main package that is built but not shipped: source/update/testdata/releasefixture/main.go (inside update's module, ignored by `./...`, built by .github/workflows/release-tool-selftest.yml; path-filtered, not a merge gate).
- Milestone "update v0.2.0: hardened updates" (milestone 5) has open: #94 (release update), #67 (this guide), #10 (epic). Signature, windows, channels, preflight, reusable release workflow (#58-#66) are closed, so the consumer release contract (ADR-0024/0026/0027/0032/0033) is settled; remaining change is the update release itself.

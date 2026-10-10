---
about: "go.work masks unreleased sibling requirements; CI uses GOWORK=off so a project must require a sibling version containing what it uses, making cross-project changes two ordered releases"
saw:
  - docs/adr/0002-a-cross-project-dependency-is-a-released-version-linked-locally-by-go-work.md
  - agentic/rules/a-sibling-is-required-at-a-released-version.md
  - go.work
---

Kind: gotcha / rationale.

Locally everything builds because go.work resolves siblings from disk; CI and release set GOWORK=off and build from each go.mod/go.sum. So code in `b` using a not-yet-released API of `a` passes locally and fails CI. Required order: merge and tag `a`, then in `b` run `go get github.com/vipengele/go/source/a@vX.Y.Z` (not hand-editing go.mod, so go.sum follows) and make the change. Never `replace` (ignored by consumers, so builds here and fails everywhere else) and never pseudo-versions; `verify-sibling-deps` also fails a sibling version with no tag.
Also: change detection selects dependents transitively, so a `cli` or `update` change builds/tests `daemon`. Every new project needs its `use` line in go.work, a `.lydite/components.yml` component and a `.gt-repo.yaml` gomod entry (+ `gt repo sync`) or CI/dependabot silently skip it.
Rejected: CI in workspace mode (tests branch code, not what the tag delivers) and vendoring siblings.
Evidence: ADR-0002, rule file, README "Add a project".

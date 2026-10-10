---
name: changed-projects-select-all-regex
kind: gotcha
description: changed-projects selects every project only for ci-[a-z]+.yml, actions/ and .lydite/; go.work, dependabot.yml, other workflows and root files select none, so build and test are skipped.
anchors:
  - path: .github/actions/changed-projects/action.yml
    blob: b03e627a7e47
confidence: verified
---

The only "shared CI changed, select all" rule is the regex at `.github/actions/changed-projects/action.yml:63`: `^(\.github/(workflows/ci-[a-z]+\.yml|actions/)|\.lydite/)`. Otherwise selection is `grep -oE '^source/[^/]+'` (`:69`), so edits to `go.work`, `.gt-repo.yaml`, `release.yml`, `gt-*.yml`, or a stage file with a digit/hyphen in its name (`ci-e2e2.yml`) select nothing and the gate passes without exercising them. Widen the regex when adding such a file.

Related fallback: with no usable base commit (shallow checkout, missing base) the action selects every project (`:54-58`), which hides a missing `fetch-depth: 0` (set in ci-preflight.yml:37, ci-build.yml:45, ci-test.yml:35).

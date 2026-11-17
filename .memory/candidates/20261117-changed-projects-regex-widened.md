---
about: "changed-projects select-all regex now also covers release.yml, auto-release.yml, release-selftest.yml and scripts/release/"
saw:
  - .github/actions/changed-projects/action.yml
targets: changed-projects-select-all-regex
verdict: now-false
---
`grep -n` finds the select-all rule at `action.yml:100`: `^(\.github/(workflows/(ci-[a-z]+|release|auto-release|release-selftest)\.yml|actions/|scripts/release/)|\.lydite/)`. The note claims only `ci-[a-z]+.yml`, `actions/` and `.lydite/` (line `:63`), and that `release.yml` selects nothing. Still true: `go.work`, `.gt-repo.yaml`, `gt-*.yml` and a ci file with a digit/hyphen select nothing. A new action step also rejects nested go.mod (a project dir containing another go.mod) around `:70-85`.

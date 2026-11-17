---
about: "the changed-projects select-all regex also matches the release scripts and workflows, so a change to them selects every project and runs the test stage, where ci-test.yml calls release-selftest.yml"
saw:
  - .github/actions/changed-projects/action.yml
  - .github/workflows/ci-test.yml
  - .github/workflows/release-selftest.yml
  - .github/workflows/ci-preflight.yml
---
The select-all regex in `.github/actions/changed-projects/action.yml:100` is
`^(\.github/(workflows/(ci-[a-z]+|release|auto-release|release-selftest)\.yml|actions/|scripts/release/)|\.lydite/)`.
The note `changed-projects-select-all-regex` cites the older pattern and line numbers.

- `release\.yml` is anchored, so `release-tool*.yml` stays owned by `update` (comment at `action.yml:31-33`).
- `.github/workflows/ci-test.yml` has a `release-selftest` job that calls `.github/workflows/release-selftest.yml`
  on every run of the test stage, so the release self-test gates `ci-gate` through that stage.
- A docs-only change selects no project; `ci-preflight.yml` then skips the test stage and the self-test with it.

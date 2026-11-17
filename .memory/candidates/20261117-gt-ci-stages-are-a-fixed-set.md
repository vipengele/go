---
about: "gt 2.1.1 accepts only the ci stages preflight, build, test and end2end in .gt-repo.yaml, and ci-orchestration.yml is gt-rendered, so an extra gating job goes inside a repo-owned ci-*.yml stage, never into ci-orchestration.yml"
saw:
  - .gt-repo.yaml
  - .github/workflows/ci-orchestration.yml
  - .github/workflows/ci-test.yml
---
- `gt repo sync` with `pipeline.ci.stages: [..., release-selftest]` in `.gt-repo.yaml` fails:
  `unknown stage "release-selftest" (accepted: preflight, build, test, end2end)` (gt 2.1.1, pinned by `.gt-repo.yaml:18`).
- `.github/workflows/ci-orchestration.yml:1` says it is managed by gt; the `governance` job of that workflow runs
  `gt repo check`, which fails on any hand edit ("drifted .github/workflows/ci-orchestration.yml").
- The stage files `ci-preflight.yml`, `ci-build.yml` and `ci-test.yml` say "this file is yours" (`ci-test.yml:1-4`):
  a new gating job belongs in one of them, as `release-selftest` does in `ci-test.yml`.

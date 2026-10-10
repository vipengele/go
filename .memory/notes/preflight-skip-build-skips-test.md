---
name: preflight-skip-build-skips-test
kind: gotcha
description: test needs build, so a preflight that sets only run-build=false silently skips test too; only run-test=false skips independently.
anchors:
  - path: .github/workflows/ci-orchestration.yml
    blob: f09c5de0d2ee
  - path: .github/workflows/ci-preflight.yml
    blob: aadc4360e76c
confidence: verified
---

`test` has `needs: [attest, build, preflight]` (`ci-orchestration.yml:114`) and an `if` with no status function (`:115`), so when `build` is skipped, `test` is skipped as well. Preflight emits only `false`, never `true`, and absence means run; the skip path sets both together (`ci-preflight.yml:55-56`). Keep `run-build` and `run-test` flipping together. `ci-gate` treats skipped as pass (`ci-orchestration.yml:189-194`), so a wrong skip is invisible.

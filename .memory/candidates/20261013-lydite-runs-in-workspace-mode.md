---
about: "lydite 0.4.1 test and scan do not set GOWORK=off, so .lydite/components.yml's claim that components resolve siblings at released versions, never through go.work, is false"
saw:
  - .lydite/components.yml
  - .github/workflows/ci-orchestration.yml
---

`.lydite/components.yml:8-10` says a component resolves a sibling at the released version its go.mod names, never through go.work. Checked locally with lydite v0.4.1 in a scratch repo (go.work present, app requiring unreleased `commons v0.99.0`, no replace, GOWORK unset): `lydite test` -> `test(app) passed`; `lydite scan` -> `govulncheck(app) passed`. Both could only pass with the workspace active (`GOWORK=off go build` there fails with missing go.sum entry). gosec failed there for an unrelated reason (export data version 5 > supported 4: gosec built with an older Go than 1.27).

The CI lydite stage is `pedromvgomes/gt/.github/workflows/reusable-lydite.yml@v2` (ci-orchestration.yml:180), outside this repo, and ci-orchestration passes it no env, so what it sets for GOWORK in CI is UNVERIFIED. The lydite binary does contain a `GOWORK=off` string, context unknown (near mutation/git code).

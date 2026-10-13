---
about: "lydite 0.4.1 test and scan do not set GOWORK=off, so they run with go.work active; with the directory replace a sibling resolves from the dependent's own go.mod in or out of workspace mode, and what the CI lydite workflow sets is unverified"
saw:
  - .lydite/components.yml
  - .github/workflows/ci-orchestration.yml
---

Checked locally with lydite v0.4.1 in a scratch repo (go.work present, app requiring unreleased `commons v0.99.0`, no replace, GOWORK unset): `lydite test` -> `test(app) passed`; `lydite scan` -> `govulncheck(app) passed`. Both could only pass with the workspace active (`GOWORK=off go build` there fails with missing go.sum entry). gosec failed there for an unrelated reason (export data version 5 > supported 4: gosec built with an older Go than 1.27).

So a lydite component is not guaranteed to build a dependent the way `GOWORK=off` CI does. That does not matter for a dependent with `require` plus a directory `replace` of its sibling (ADR-0006): it builds the same with the workspace on or off. `.lydite/components.yml:8-10` makes no claim about go.work.

The CI lydite stage is `pedromvgomes/gt/.github/workflows/reusable-lydite.yml@v2` (`ci-orchestration.yml:160`), outside this repo, and ci-orchestration passes it no env, so what it sets for GOWORK in CI is UNVERIFIED. The lydite binary does contain a `GOWORK=off` string, context unknown (near mutation/git code).

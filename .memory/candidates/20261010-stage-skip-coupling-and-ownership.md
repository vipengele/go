---
about: "preflight run-build/run-test must flip together because test needs build; ci-orchestration.yml is gt-managed while ci-build/test/preflight are repo-owned"
saw:
  - .github/workflows/ci-orchestration.yml
  - .github/workflows/ci-preflight.yml
  - .github/workflows/ci-build.yml
  - .github/workflows/ci-test.yml
---

Kind: invariant / rationale.

- Ownership: ci-orchestration.yml header says "Managed by gt - edit .gt-repo.yaml, then run
  `gt repo sync`"; edits are overwritten. ci-build/ci-test/ci-preflight are "yours", created once,
  never touched. dependabot.yml is also gt-managed. Branch protection requires only `ci-gate`.
- Coupling: `test` has `needs: [attest, build, preflight]` (orchestration:204). A job whose
  needed job was skipped is itself skipped unless its `if` has a status function. So setting only
  `run-build=false` in preflight silently skips test too; setting only `run-test=false` is the
  only independent skip. Preflight emits only `false` (never `true`); absence means run
  (ci-preflight.yml:337-339, 386-389).
- Contract: ci-build/ci-test must keep `on: workflow_call` and their `permissions:` block is a
  ceiling that can only narrow the caller's grant (ci-build.yml:18-31).
- `lydite` deliberately has no skip guard (`if: "!cancelled()"`) and `lydite-baseline` is
  deliberately absent from `ci-gate` needs (orchestration:239-275): do not "tidy" these into
  the guarded form - it stalls merge queues / blocks main on a baseline failure.
- ci-gate treats skipped as pass; empty-matrix jobs are guarded with `projects != '[]'` because
  an empty matrix is a workflow error.

What breaks: edits lost on next sync; a lone run-build=false skips tests; a "consistency" edit to lydite stalls the merge queue.
Evidence: reading the four workflows; GitHub needs/skip semantics.

---
about: "lydite clearance is its own issue_comment workflow because issue_comment runs the default branch's workflow copy; lydite/referral is published only by the unguarded lydite job"
saw:
  - .github/workflows/gt-lydite-clearance.yml
  - .github/workflows/ci-orchestration.yml
  - .lydite/config.yml
  - .lydite/components.yml
---

Kind: rationale.

- `gt-lydite-clearance.yml:4-10`: an `issue_comment` run always executes the DEFAULT branch's copy of workflows, never the PR's. So a change to this file cannot be tested from a PR, and it must not be merged into the pull_request-triggered ci-orchestration.yml (that would run the whole pipeline on every comment). `/lydite clear` unblocks the `lydite/referral` status branch protection requires.
- `ci-orchestration.yml:147-166`: the `lydite` job has `if: !cancelled()` with no skip guard on purpose: it is the only publisher of `lydite/referral`, which must exist for every SHA (including merge_group commits). Adding a skip guard stalls the merge queue on a check nothing can report. `lydite-baseline` (push-only, `needs: [attest]`) is intentionally absent from ci-gate's needs (lines 172-188, 193).
- `.gt-repo.yaml:24-29` lists pipeline stages preflight/build/test only; lydite and governance come from gt defaults, so do not look there to add or remove them.
- `.lydite/components.yml`: one `go-test` component per module, rooted at its dir, with `-race`. A new project must add a component or the lydite coverage gate never exercises it (CLAUDE.md also requires a gomod entry in .gt-repo.yaml and go.work `use`). Siblings resolve at released versions, so no `setup`/`depends_on` is needed (lines 8-11).
- `.lydite/config.yml:2-3`: licence allow-list is Apache-2.0, BSD-2/3, ISC, MIT, Unicode-3.0; a dependency under any other licence (e.g. MPL, GPL) fails the lydite stage.

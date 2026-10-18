---
about: "cloudflare-production environment only admits refs matching the tag policy tools/vanity-worker/v*, so a branch or push-to-main deploy could not reach CLOUDFLARE_API_TOKEN without changing the environment, not just the workflow"
saw:
  - .github/workflows/deploy-vanity-worker.yml
  - .github/workflows/release.yml
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
---

Evidence (live repo settings, read with `gh api`, 2026-10-17; re-check, they are not in git):

- `gh api repos/vipengele/go/environments/cloudflare-production/deployment-branch-policies` -> one policy, `name: tools/vanity-worker/v*`, `type: tag`. `deployment_branch_policy: {protected_branches:false, custom_branch_policies:true}`, `can_admins_bypass:false`, no required reviewers or wait timer (only a `branch_policy` protection rule). The secret `CLOUDFLARE_API_TOKEN` is an environment secret (repo-level `actions/secrets` is empty); `CLOUDFLARE_ACCOUNT_ID` is read as `vars.`.
- So the deploy job on `refs/heads/main` would be refused by the environment before any step runs. Adding a push-to-main deploy needs a second policy (`main`, branch type) on the environment: that widens who can reach the token to anything merged to main.
- Workflow header `deploy-vanity-worker.yml:8-11`: "A deploy runs only from a tag ... that points at a commit on main; no branch is ever deployed." Header `:13-18`: three jobs, seams are safety properties: `tag` holds no credential (`permissions: contents: read`, `:44-45`), `deploy` is the only job in the environment (`:93-95`, `contents: read`), `announce` is the only writer (`contents: write`, `:171-172`). Workflow-level `permissions: {}` (`:38`) makes every job opt in.
- The tag is the review point: the `tag` job checks the regex (`:64`), checks out `refs/tags/<tag>` (`:73-77`) and requires `git merge-base --is-ancestor HEAD origin/main` (`:84`). The Worker is built from the tagged commit, so deploy content equals what was merged and tagged. `workflow_dispatch` redeploys/rolls back an existing tag only, never a branch.
- Credentials are absent from install/build steps and present only in `Deploy` (`:139-143`). `deploy` has `concurrency: group deploy-vanity-worker, cancel-in-progress: false` (`:100-102`).
- No ADR records any of this: grep of docs/adr for Worker/deploy finds only ADR-0004 (what the Worker is, "deployed from this repository's CI", and the consequence "A new project needs a Worker deploy before its first tag"). The rationale lives only in the workflow header and commit f0c3161 (empty body).
- No tag ruleset or tag protection exists: `gh api repos/vipengele/go/rulesets` lists one ruleset, `gt`, target `branch`, include `refs/heads/main` (deletion, non_fast_forward, linear history, PR with squash only and 0 approvals, required checks `ci-gate` and `lydite/referral`, no bypass actors). `.../tags/protection` -> 404. Branch protection (classic) -> 404. Anyone with write access can push any `source/*` or `tools/*` tag.

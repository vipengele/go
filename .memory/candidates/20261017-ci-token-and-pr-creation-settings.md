---
about: "repository Actions settings: default GITHUB_TOKEN is read-only and workflows may not create or approve PRs; no GitHub App secret is configured; tags pushed by GITHUB_TOKEN do not start release.yml"
saw:
  - .github/workflows/gt-sync.yml
  - .github/workflows/dependabot-auto-merge.yml
  - .github/workflows/release.yml
  - .github/workflows/deploy-vanity-worker.yml
---

Live settings (`gh api`, 2026-10-17; not in git, re-check): `repos/vipengele/go/actions/permissions/workflow` -> `default_workflow_permissions: read`, `can_approve_pull_request_reviews: false`. That second flag is the "Allow GitHub Actions to create and approve pull requests" switch, so a workflow that runs `gh pr create` with GITHUB_TOKEN is refused until it is enabled. Repo secrets: none (`actions/secrets` total 0), so `APP_CLIENT_ID` / `APP_PRIVATE_KEY` (passed by name in `dependabot-auto-merge.yml:29-38`) are unset and the reusable workflows treat that as "not configured": no bot App exists. Variable: only `GT_LYDITE_RELAY`.

Existing PR-opening workflow: `gt-sync.yml:19-21` grants `contents: write` and `pull-requests: write` and opens a PR via gt's reusable workflow, never pushing to main (`:3-5`). PRs opened with GITHUB_TOKEN do not trigger `pull_request` workflows, so a PR whose required checks (`ci-gate`, `lydite/referral`, ruleset `gt`) never start cannot merge without a human pushing or closing/reopening; this is documented GitHub behaviour and is not evidenced as tried here (I did not find a note on it).

Same limitation for tags: a tag pushed by a workflow with GITHUB_TOKEN does not trigger `release.yml` (`on: push: tags`) or `deploy-vanity-worker.yml`; only a PAT or App token does. Documented GitHub behaviour, not exercised in this repo. `release.yml:3` states "A human pushes the tag; nothing else starts this."

GITHUB_TOKEN cannot touch `.github/workflows/**` (see gt-token-cannot-touch-workflows); the human's own gt credentials can (`gt repo fleet sync`, `merge-pending`).

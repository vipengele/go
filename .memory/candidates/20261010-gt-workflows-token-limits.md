---
about: "GITHUB_TOKEN cannot touch .github/workflows/**, so gt-sync does not repair workflow drift and Dependabot actions bumps never auto-merge"
saw:
  - .github/workflows/gt-sync.yml
  - .github/workflows/dependabot-auto-merge.yml
---

Kind: gotcha.

- `gt-sync.yml:7-11`: the weekly sync opens a PR for drift but deliberately excludes workflow files, because GITHUB_TOKEN cannot create or update anything under .github/workflows/**. Workflow drift is only reported; a human must run `gt repo fleet sync` locally. It never pushes to main.
- `dependabot-auto-merge.yml:9-12`: Dependabot PRs touching .github/workflows/** (the whole github-actions ecosystem) can never be merged by that job (no `workflow` scope). They need a human or `gt repo fleet merge-pending`. This is a known gap, not a bug; do not "fix" it by widening permissions. Bumps above `minor` are also left for human review (line 7).
- `dependabot-auto-merge.yml:29-38`: secrets are passed by name, NOT `secrets: inherit`, because gt lives under a different owner (pedromvgomes) than the calling repos, and inherit only works within an org/enterprise. Switching to inherit silently drops APP_CLIENT_ID/APP_PRIVATE_KEY (the lydite stage lost its Codecov token this way). Both secrets are optional; empty means "not configured".
- Permissions at the caller (lines 20-24) must include the writes, since a reusable workflow can only narrow the token.

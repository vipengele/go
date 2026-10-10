---
name: gt-token-cannot-touch-workflows
kind: gotcha
description: GITHUB_TOKEN cannot modify .github/workflows/**, so gt-sync never repairs workflow drift and Dependabot github-actions bumps never auto-merge.
anchors:
  - path: .github/workflows/gt-sync.yml
    blob: 98feefdda65e
  - path: .github/workflows/dependabot-auto-merge.yml
    blob: 80f3a8ff277c
confidence: verified
---

`gt-sync.yml:7-11`: the weekly sync PR excludes workflow files, since GITHUB_TOKEN cannot create or update anything under `.github/workflows/**`; drift is only reported and needs a local `gt repo fleet sync`. `dependabot-auto-merge.yml:9-12`: Dependabot PRs touching workflows (the whole github-actions ecosystem) can never be merged by that job; they need a human or `gt repo fleet merge-pending`. A known gap, not a bug: do not widen permissions. Bumps above minor are always left for review (`:7`). See [[dependabot-auto-merge-secrets-named-not-inherited]].

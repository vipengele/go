---
name: dependabot-auto-merge-secrets-named-not-inherited
kind: gotcha
description: "gt reusable workflows get secrets by name, not secrets: inherit, because gt lives under a different owner and inherit silently drops them."
anchors:
  - path: .github/workflows/dependabot-auto-merge.yml
    blob: 80f3a8ff277c
confidence: verified
---

`dependabot-auto-merge.yml:29-38`: `secrets: inherit` only works within one organization or enterprise, and gt is owned by pedromvgomes, so APP_CLIENT_ID and APP_PRIVATE_KEY are passed explicitly. Switching to inherit silently drops them (the lydite stage lost its Codecov token this way). Both are optional; empty means "not configured". Callers must also grant the write permissions themselves (`:20-24`), since a reusable workflow can only narrow the token.

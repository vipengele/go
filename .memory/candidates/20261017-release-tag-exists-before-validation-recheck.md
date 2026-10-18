---
about: "release-tag-exists-before-validation still holds; line numbers shifted"
saw:
  - .github/workflows/release.yml
targets: release-tag-exists-before-validation
verdict: still-true
---

Trigger is `on: push: tags: ['source/**/v[0-9]*.[0-9]*.[0-9]*']` (`release.yml:18-24`, not `:18-22`). The tag job's checks are now `:57-183` (notes check `:96-100`, comment `:52-56` still claims nothing is left half-released). `actions/checkout` is `:47` (default ref, the tagged commit); the idempotent `gh release view` guard is `:262-265`, not `:154-157`. Re-running still checks out the tagged commit, so missing notes cannot be repaired after tagging.

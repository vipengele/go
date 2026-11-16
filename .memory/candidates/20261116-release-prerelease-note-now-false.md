---
about: "release-prerelease-tags-always-fail mechanism changed: the suffix check is inline in release.yml, verify-sibling-deps is no longer called by release.yml"
saw:
  - .github/workflows/release.yml
  - .github/actions/verify-sibling-deps/action.yml
targets: release-prerelease-tags-always-fail
verdict: now-false
---
Re-checked 2026-11-16. release.yml no longer passes `expect-tag` to verify-sibling-deps (action has no inputs; release.yml does its own checks inline :121-183). The tag regex still accepts `-suffix` (:61) and the prerelease flag still exists (:284), but the failure now comes from the inline check at release.yml:131-134 ("carries a prerelease suffix"), after the tag is pushed. Outcome (rc tags fail) holds; the pointers `release.yml:90`, `action.yml:63` are wrong. Same drift for release-v2-tags-fail-announce: module is now `go.vipengele.org/${project}` (release.yml:104, no /vN), announce checks at :263; verify-sibling-deps compares to `go.vipengele.org/<dir>` (action.yml:89-94). Claim of no /vN handling is still-true, strings and lines differ. release-tag-exists-before-validation: still-true; trigger :18-24, checks :57-100, release idempotence :279.

---
name: verify-sibling-deps-tag-check-unauthenticated
kind: gotcha
description: verify-sibling-deps checks sibling tags with an unauthenticated git ls-remote and treats any failure as 'no tag', so a private repo fails every sibling require.
anchors:
  - path: .github/actions/verify-sibling-deps/action.yml
    blob: 31eb1af43f5e
confidence: verified
---

`.github/actions/verify-sibling-deps/action.yml:55` runs `git ls-remote --exit-code --tags https://github.com/${GITHUB_REPOSITORY}.git refs/tags/source/<sib>/<ver>` with stderr to /dev/null and no credentials. Private repo or network failure is indistinguishable from a genuinely unreleased version. Pseudo-versions (`:53`) and `replace` of a sibling path (`:46`) are rejected; go.work is not inspected, since CI runs GOWORK=off.

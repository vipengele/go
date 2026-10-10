---
name: release-v2-tags-fail-announce
kind: gotcha
description: release.yml builds the module path without a /vN suffix, so a correctly-pathed v2+ module fails the announce job's proxy check.
anchors:
  - path: .github/workflows/release.yml
    blob: 3ff417587ee8
  - path: .github/actions/verify-sibling-deps/action.yml
    blob: 31eb1af43f5e
confidence: verified
---

`release.yml:83` sets `module=github.com/${GITHUB_REPOSITORY}/source/${project}` with no major suffix; `announce` runs `go list -m "${MODULE}@${VERSION}"` (`release.yml:138`) with 6 retries. For a v2.0.0 whose go.mod path correctly ends `/v2`, the proxy rejects that query, the run goes red and the GitHub Release is never created, while the tag is already pushed. The `tag` job regex (`release.yml:58`) accepts v2 without complaint. Likewise `verify-sibling-deps` compares each `go.mod` path to exactly `github.com/vipengele/go/source/<dir>` (`action.yml:36-41`), so a `/v2` path is reported as a mismatch. Fix both to derive `/vN` before tagging a first v2.

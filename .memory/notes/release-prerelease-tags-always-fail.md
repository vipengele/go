---
name: release-prerelease-tags-always-fail
kind: gotcha
description: A pre-release tag like source/x/v1.0.0-rc.1 passes release.yml's tag regex but verify-sibling-deps rejects it, after the tag is already pushed.
anchors:
  - path: .github/workflows/release.yml
    blob: 3ff417587ee8
  - path: .github/actions/verify-sibling-deps/action.yml
    blob: 31eb1af43f5e
confidence: verified
---

`release.yml:58` accepts an optional `-suffix` and `release.yml:159` flags `-*` versions as GitHub prereleases. But the `tag` job's last step passes `expect-tag` to `verify-sibling-deps` (`release.yml:90`), whose regex at `.github/actions/verify-sibling-deps/action.yml:63` has no suffix group, so any `-rc.N` tag fails with "is not source/<project>/vX.Y.Z". The suffix branches in release.yml are dead; only `v0.*` reaches `--prerelease`. Widen the action regex or drop the suffix from release.yml before cutting an rc. The tag is already pushed when this fails, see [[release-tag-exists-before-validation]].

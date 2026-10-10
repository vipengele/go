---
name: release-tag-exists-before-validation
kind: gotcha
description: release.yml is triggered by the tag push, so the tag already exists when its validation fails, and re-running checks out the tagged commit so missing release notes cannot be fixed.
anchors:
  - path: .github/workflows/release.yml
    blob: 3ff417587ee8
confidence: verified
---

The workflow triggers on `push: tags: source/*/v...` (`release.yml:18-22`). The `tag` job's checks (project exists, commit on main, notes file `docs/release-notes/<project>/<version>.md`; `release.yml:50-79`) therefore run after the tag is on the remote, despite the comment claiming nothing is left half-released. `actions/checkout` (`release.yml:45`) checks out the tagged commit, so merging the notes afterwards and re-running still fails: the file is absent at that commit. Write and merge notes first, then tag from merged main. Re-run only helps for failures after the tag job, e.g. proxy lag; the GitHub Release step is idempotent (`release.yml:154-157`).

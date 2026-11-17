---
about: "release-prerelease-tags-always-fail and release-v2-tags-fail-announce: pointers moved, release.yml restructured, claims unchecked"
saw:
  - .github/workflows/release.yml
  - .github/actions/verify-sibling-deps/action.yml
targets: release-v2-tags-fail-announce
verdict: unchecked
---
release.yml was reworked (tags now created by auto-release.yml, ADR-0037; the `module=` assignment the note cites at `:83` and `go list -m` at `:138` are not at those lines; announce step is near `:241-271`). A grep for prerelease/rc/suffix finds no hits, so the prerelease note's `:58/:90/:159` pointers are dead. Not re-verified in this task whether a v2 `/vN` module path or an rc tag still fails. The shape of the notes should be re-read by the curator.

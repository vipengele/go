---
about: "a -rc.N tag still fails release.yml, but the rejecting check is now inline in the tag job, not in verify-sibling-deps"
saw:
  - .github/workflows/release.yml
  - .github/actions/verify-sibling-deps/action.yml
targets: release-prerelease-tags-always-fail
verdict: still-true
---

The effect holds, the mechanism in the note does not. `release.yml:61` regex still accepts `(-[0-9A-Za-z.-]+)?`, but the `tag` job no longer passes `expect-tag` to the action; the action has no tag regex (grep `expect-tag` in the action -> 0 hits). The rejection is the inline check `release.yml:131-134` (`^v[0-9]+\.[0-9]+\.[0-9]+$`, "carries a prerelease suffix"), after the tag is pushed. The `*-*` prerelease branch at `:267` is dead; only `v0.*` reaches `--prerelease`. ADR-0006 Consequences records this.

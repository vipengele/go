---
about: "a -rc.N version is still rejected, but in verify's validate before any tag is pushed, by release-project.sh, not release.yml or verify-sibling-deps"
saw:
  - .github/workflows/release.yml
  - .github/scripts/release/release-project.sh
  - .github/scripts/release/lib.sh
  - .github/actions/verify-sibling-deps/action.yml
targets: release-prerelease-tags-always-fail
verdict: now-false
---

Re-checked 2026-11-17. The outcome (no prerelease-suffixed release can be cut) holds; every pointer and two claims in the note are wrong.

- Regex: `RELEASE_TAG_RE` in `.github/scripts/release/lib.sh:32` (and a copy at `release.yml:132`) still accepts `(-[0-9A-Za-z.-]+)?`, deliberately, so that the script can name the problem (`release.yml:129-131`).
- Rejection: `release-project.sh:189-191`, `^v[0-9]+\.[0-9]+\.[0-9]+$` else "carries a prerelease suffix; only source/<project>/vX.Y.Z is released", inside `check()` run by `validate` (and `finish-check`) in the `verify` job. No tag exists yet in mode `release`, so nothing is left half-released; the note's last sentence ("the tag is already pushed when this fails") is false, as is the `[[release-tag-exists-before-validation]]` link (itself superseded). In `finish` mode on a manually pushed suffixed tag the same check refuses it too.
- verify-sibling-deps is not involved: release.yml does not call it (grep `verify-sibling-deps` in release.yml -> 0 hits) and the action has no `expect-tag` input.
- Dead branch: `release-project.sh:369` `if [[ "$version" == *-* || "$version" == v0.* ]]` -> `--prerelease`; the `*-*` half is unreachable, only `v0.*` reaches it. Dropping the suffix from the regexes or relaxing line 189 is needed before an rc can be cut.

---
about: "pre-release tags (vX.Y.Z-rc.1) pass the tag job's regex but are always rejected by verify-sibling-deps"
saw:
  - .github/workflows/release.yml
  - .github/actions/verify-sibling-deps/action.yml
---

Kind: gotcha.

`release.yml:58` accepts an optional pre-release suffix `(-[0-9A-Za-z.-]+)?`, and `:159` marks `-*` versions as GitHub prereleases. But the `tag` job's last step calls `verify-sibling-deps` with `expect-tag` (`:90-92`), whose regex in `.github/actions/verify-sibling-deps/action.yml` (the `EXPECT_TAG` block) is `^source/([a-z0-9-]+)/(v[0-9]+\.[0-9]+\.[0-9]+)$` with no suffix. Any `-rc.1` tag therefore fails with "is not source/<project>/vX.Y.Z". The prerelease branches in release.yml are dead for suffixed versions; only `v0.*` reaches the prerelease flag.

What breaks: someone cuts `v1.0.0-rc.1` expecting the documented pipeline and gets a red run with a tag that is already pushed. Either widen the action regex or drop the suffix from release.yml.

Related: the action checks every `source/*/go.mod` in the repo, not just the released project, so another project's bad sibling require blocks this release too. Sibling tag existence is checked with `git ls-remote` over unauthenticated https to the repo URL, so it assumes a public repo.

---
about: "A prerelease tag such as source/x/v1.0.0-rc.1 passes release.yml's tag-form regex but fails the tag job's own go.mod step; the suffix branch in the GitHub Release step only ever marks v0.* releases as prereleases"
saw:
  - .github/workflows/release.yml
targets: release-prerelease-tags-always-fail
verdict: still-true
---

The note's conclusion holds, its mechanism does not. `release.yml:61` accepts an optional `-suffix`, but the `tag` job's step "The tagged go.mod is what a consumer gets" (`:131-134`) fails any version that is not exactly `vX.Y.Z`. `verify-sibling-deps` takes no tag input and plays no part in it. The tag is already pushed when this fails.

Because that step fails first, the `-*` branch of the GitHub Release step (`:267`) is never reached for a suffixed version; only `v0.*` marks a release as a prerelease. A release candidate needs both the step's version check and the suffix branch revisited together.

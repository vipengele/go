---
about: "release.yml cannot release a v2+ version: the module path it checks has no /v2 suffix"
saw:
  - .github/workflows/release.yml
  - agentic/skills/release/SKILL.md
  - agentic/rules/module-path-matches-its-directory.md
---

Kind: gotcha.

`release.yml:83` writes `module=github.com/${GITHUB_REPOSITORY}/source/${project}` with no major suffix, and `announce` (`release.yml:138`) runs `go list -m "${MODULE}@${VERSION}"`. The `tag` job regex (`:58`) happily accepts `v2.0.0`. The rule file says a v2+ module path is `.../source/<project>/v2` while the tag stays `source/<project>/v2.0.0`.

So for a correctly-pathed v2 module, `go list -m .../source/<project>@v2.0.0` is rejected by the proxy (path lacks /v2) and `announce` fails after all 6 retries. The tag is already pushed and immutable, and the GitHub Release is never created. Nothing in the workflow computes the suffix. The release skill (SKILL.md step 1) says only "stop and confirm with the user" for v2, not that the workflow needs a change first.

What breaks: a human tags v2.0.0 believing the pipeline handles it; the module is valid but the release run is red and the Release missing. Fix the workflow (derive `/vN` from the version) before tagging the first v2.

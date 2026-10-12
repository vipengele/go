---
about: "release.yml accepts only a one-segment project in the tag and derives the module path from the repository, so a nested project or a vanity-path project cannot be released by it"
saw:
  - .github/workflows/release.yml
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
---

`.github/workflows/release.yml:58` matches the tag against `^source/([a-z0-9-]+)/(v...)$`, so a
three-segment tag such as `source/<dir>/<sub>/v0.0.1` is rejected as a malformed tag. `:65` then
requires `source/${project}/go.mod`, and `:83` sets the module to
`github.com/${GITHUB_REPOSITORY}/source/${project}`, which is not the module line of a project with a
vanity path (`go.vipengele.org/<dir>`, ADR-0004). Releasing a nested or vanity-path project needs all
three lines changed; pushing such a tag by hand runs no release job, and a version of a vanity-path
module pushed that way resolves through the proxy and the checksum database without any workflow.

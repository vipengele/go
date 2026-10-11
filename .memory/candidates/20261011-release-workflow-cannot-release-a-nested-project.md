---
about: "release.yml accepts only a one-segment project in the tag and derives the module path from the repository, so a nested project or a vanity-path project cannot be released by it"
saw:
  - .github/workflows/release.yml
  - source/spike/probe/go.mod
---

`.github/workflows/release.yml:58` matches the tag against `^source/([a-z0-9-]+)/(v...)$`, so
`source/spike/probe/v0.0.1` is rejected as a malformed tag. `:65` then requires
`source/${project}/go.mod`, and `:83` sets the module to
`github.com/${GITHUB_REPOSITORY}/source/${project}`, which is not the module line of a project with a
vanity path such as `go.vipengele.org/spike/probe`. Releasing a nested or vanity-path project needs
all three lines changed; pushing the tag by hand runs no release job.

---
about: "release.yml releases a project at any depth from a tag source/<path>/vX.Y.Z but derives the module path from the repository, so a project whose module line is a vanity path cannot pass its proxy step"
saw:
  - .github/workflows/release.yml
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
---

`.github/workflows/release.yml:60` matches the tag against a project of one or more `/`-separated
segments, so `source/<dir>/<sub>/v0.0.1` is a release of the project `<dir>/<sub>` and its notes are
read from `docs/release-notes/<dir>/<sub>/v0.0.1.md`. `:67` requires `source/${project}/go.mod`, and
`:72-88` reject a tag whose project directory has a `go.mod` in a parent directory or below it.
`:103` sets the module to `github.com/${GITHUB_REPOSITORY}/source/${project}`, which is the module
line of a project that keeps the repository path and not of one with a vanity path
(`go.vipengele.org/<dir>`, ADR-0004): the `announce` job's `go list -m ${MODULE}@${VERSION}` against
the proxy fails for such a project until that line is derived from the `go.mod` instead. Pushing the
tag of a vanity-path project by hand still resolves through the proxy and the checksum database
without any workflow.

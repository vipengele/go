---
about: "release.yml releases a project at any depth from a tag source/<path>/vX.Y.Z and takes go.vipengele.org/<path> as its module path, so its announce step fails until the vanity Worker answers for that path"
saw:
  - .github/workflows/release.yml
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
---

`.github/workflows/release.yml:60` matches the tag against a project of one or more `/`-separated
segments, so `source/<dir>/<sub>/v0.0.1` is a release of the project `<dir>/<sub>` and its notes are
read from `docs/release-notes/<dir>/<sub>/v0.0.1.md`. `:67` requires `source/${project}/go.mod`, and
`:72-88` reject a tag whose project directory has a `go.mod` in a parent directory or below it.
`:103` sets the module to `go.vipengele.org/${project}`, and the `announce` job runs
`go list -m ${MODULE}@${VERSION}` through the module proxy. The proxy resolves that path through the
vanity Worker, which answers only for the directories it was deployed with, so the step fails for a
project whose `go.mod` the deployed Worker does not yet list; the failure message says to deploy the
Worker and re-run. Pushing the tag first is allowed (the workflow is re-runnable), but until the
Worker knows the path nothing can resolve the version.

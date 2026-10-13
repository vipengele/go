---
about: "ci-build runs verify-sibling-deps repo-wide on every PR, in development or per-project strict mode; release.yml's tag job does its own check scoped to the tagged project, so an unreleased sibling elsewhere never blocks a release; the test job builds tidy as a consumer would, but a GOWORK=off build alone cannot see a leftover replace"
saw:
  - .github/actions/verify-sibling-deps/action.yml
  - .github/workflows/release.yml
  - .github/workflows/ci-build.yml
---

`verify-sibling-deps/action.yml` loops `find source -name go.mod` (`:76`) and decides per project: strict while a release-notes file has no tag, otherwise development (a version-less replace to the sibling's own directory is allowed). It is called by `ci-build.yml:48` (detect job, every PR, whole repo) only. `expect-tag` (`:178-185`) validates the tag form and that the project exists.

`release.yml` `tag` job does NOT call the action. Its step "The tagged go.mod is what a consumer gets" reads only `source/<tagged project>/go.mod`: it fails any replace (sibling or not), a sibling pseudo-version, a sibling version with no tag, a module path other than `go.vipengele.org/<project>`, and a prerelease suffix, using one unauthenticated `git ls-remote` (`:160`) made only when the project requires a sibling. Other projects' go.mod files are never read, so an unreleased sibling or a replace elsewhere does not block the release.

Why the explicit check is needed (verified in a scratch module pair): a `replace => ../sibling` left in go.mod is honoured by `GOWORK=off go build` of the main module, so building cannot detect it, while every consumer of the tag ignores it. The workflow-level `env: GOWORK: "off"` (`:36`) means the `test` job sees the module without the workspace. That job runs `go mod tidy` and fails if `go.mod` or `go.sum` change (`:200-212`), then `go build ./...`, `go vet`, `go test -race -shuffle=on` (`:214-216`). With an unreleased sibling version and no replace, tidy and build FAIL (404 / missing go.sum), which is the consumer-view check that the released sibling contains the API used; the tag-existence check alone does not prove that.

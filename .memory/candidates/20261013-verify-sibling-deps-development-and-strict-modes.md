---
about: "verify-sibling-deps has a development mode (version-less directory replace of the sibling accepted) and a state-based strict mode (release notes without a tag: no replace, tagged siblings only); tags are listed once, unauthenticated, and a failed listing stops the check"
saw:
  - .github/actions/verify-sibling-deps/action.yml
  - .github/workflows/release.yml
  - docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md
targets: verify-sibling-deps-tag-check-unauthenticated
verdict: now-false
---

The note states the old rule: a `replace` of a sibling is always rejected, a sibling `require` is always looked up as a tag, and a failed `git ls-remote` reads as "no tag". None of that holds. ADR-0006 replaces it.

`.github/actions/verify-sibling-deps/action.yml` checks every `source/**/go.mod`, per project, in one of two modes. The mode is read from repo files and tags, never from a diff, so `pull_request`, `merge_group`, `push` and `workflow_dispatch` answer alike.

- Strict (`:120-124`): some `docs/release-notes/<project>/v<version>.md` (direct children of the project's notes directory only) has no tag `source/<project>/v<version>`. Then the go.mod may replace NO sibling and every required sibling version must be tagged. A notes file whose version is tagged leaves the project in development mode.
- Development: a replace of a sibling is accepted when its `New.Path`, resolved against the go.mod's directory with `realpath -m`, is exactly `source/<sibling>` and neither side names a version (the Go command already refuses a version on a directory target). A module replace (`=> path vN`), a replace elsewhere, and a replace of a non-sibling fail. A sibling `require` with a replace is never looked up, so it may be `v0.0.0`; without a replace it must have the tag `source/<sibling>/<version>`.
- Both modes: a sibling at a pseudo-version fails (`:160`, with or without a replace); module path must be `go.vipengele.org/<dir>` (`:103`); nested go.mod pairs fail.

Tags come from ONE unauthenticated `git ls-remote --tags --refs https://github.com/${GITHUB_REPOSITORY}.git` (`has_tag`, `:59-72`), cached for the run and made only when a verdict needs it. A failed listing is an `::error::` and `exit 1`: it is NOT read as "no tag", so a private repository fails the whole check rather than reporting every sibling as unreleased. `expect-tag` (`:178-185`) only validates the tag form and that the project exists.

`release.yml`'s `tag` job no longer calls this action. It runs its own check on the tagged project only (replace of any module, pseudo-version, untagged sibling, module path, prerelease suffix; its own single `git ls-remote` at `:160`), so an unreleased sibling elsewhere does not block a release. Its `test` job runs `go mod tidy` with a `git status --porcelain -- go.mod go.sum` check, then `go build`, `go vet`, `go test` (`:200-216`). `ci-build.yml:48` still runs the action in the detect job on every PR.

Out of scope, unfixed: the unauthenticated listing fails on a private repository; `-rc.N` and `/v2` tags fail the `tag` job; a `/v2` sibling path does not resolve to a directory.

Day-one state: `docs/release-notes/commons/v0.1.0.md` exists with no `source/commons/v0.1.0` tag, so `commons` is strict, which is harmless while it has no sibling requires.

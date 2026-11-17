---
name: release
description: >-
  Use this skill when the user asks to release, publish, cut or tag a version of
  a project in this repo (e.g. "release cli 0.2.0", "cut a patch of cli").
  Covers choosing the version, the release order when projects depend on each
  other, writing the release notes, merging them, and verifying the release.
---

# Release a project

A release is one tag for one project: `source/<project>/vX.Y.Z`. It releases that project alone
(ADR-0001). `<project>` is the project's directory relative to `source/` and may contain slashes
(`cli/core`). The module path is `go.vipengele.org/<project>`; the tag carries `source/` because it
names the repository directory. The tag is the publication — the Go module proxy serves the module
from it, resolving the path through `go.vipengele.org` — so there is no upload step and no
credential.

There is no release tooling that bumps files. The input is the merge of the pull request that adds the
release notes: `auto-release.yml` then tags the project as `github-actions[bot]` (an unsigned
annotated tag) and announces it (ADR-0037). No one pushes the tag.

## 1 — Decide the version

The tags and the full history have to be local first. A shallow clone, which every cloud checkout
is, holds no tags and a truncated history: the tag lookup below then prints nothing and `git log`
reads only the commits that were fetched, so the bump is chosen from the wrong commits and nothing
fails.

```bash
git fetch --tags origin
if [ "$(git rev-parse --is-shallow-repository)" = true ]; then git fetch --unshallow origin; fi
```

Read `git log` for `source/<project>/` since the project's last tag
(`git tag --list 'source/<project>/v*' --sort=-v:refname | head -1`) and pick the bump from the
Conventional Commit types: a `!` or `BREAKING CHANGE` is major, `feat` is minor, anything else is
patch. Before `v1.0.0` a breaking change is a minor bump. A project with no tag is released for the
first time, and its whole history counts.

A major version of `v2` or above changes the module path (`agentic/rules/module-path-matches-the-vanity-map.md`).
Stop and confirm with the user before releasing one.

## 2 — Release the siblings it needs with it

List the siblings the project requires:

```bash
cd source/<project>
go mod edit -json | jq -r '.Require[]? | select(.Path | startswith("go.vipengele.org/")) | "\(.Path) \(.Version)"'
```

Read the `go.mod` this way, not with `go list -m all`: that runs in workspace mode while `go.work`
is present, and lists every project in the workspace as though it were required.

The release needs each sibling released at a version that contains what the project uses. A sibling
whose latest tag already does stays at that tag. A sibling whose latest tag lacks something the
project uses is released together with it: decide its version and write its notes with this same
skill, in the same pull request. `auto-release` tags it in an earlier tier than the project, and tags
the project on a release commit that requires it at that version (ADR-0038).

The order matters only at release time. No sibling is ever released so that a dependent can be
developed, and a project nothing unreleased feeds (a patch, or a project no sibling uses) is
released on its own whenever it is ready.

## 3 — Write the release notes

`docs/release-notes/<project>/v<version>.md`. **It is the release: merging it to `main` releases the
project, and the workflow fails without it**, before it tags anything. Say what changed and what a consumer has to do about it. A breaking change names
the old and new spelling; "various fixes" is not release notes.

The notes put the project into strict mode until its tag exists: `verify-sibling-deps` then fails,
on every change, a sibling required at a version that is neither pending (notes exist, tag does not)
nor tagged, and a `replace` that is not the sibling's own directory without a version.

## 4 — Require the siblings in the same change

A sibling developed together with the project is replaced by its directory
(`agentic/rules/a-sibling-is-developed-by-a-directory-replace.md`). Keep the `replace` and require
the sibling at the version it is released at, which is the version of its own notes in this pull
request when it is released with the project, or its latest tag:

```bash
cd source/<project>
go mod edit -require=go.vipengele.org/<sibling>@v<sibling-version>
go mod tidy
```

The notes of the project and of every sibling released with it, the raised `require`s and the tidied
`go.sum` are one release pull request, `chore(<project>): release <version>`. `auto-release`
releases the siblings first, then builds the project's release commit, the merge commit with only
its `go.mod` and `go.sum` changed, the `replace`s dropped and the module tidied against the
siblings' tags, and tags that commit. `main` keeps the `replace`. A dry run of a chain fails at the
dependent, because the sibling's tag does not exist.

## 5 — Check it locally, then merge it

This check is mandatory, on the release branch before merging. A `GOWORK=off` build honours the
project's own `replace`, so only reading `go.mod` catches one that is not a sibling's own directory,
and a tag is permanent. A sibling's directory `replace` may stay: the release commit drops it.

```bash
cd source/<project>
export GOWORK=off
go mod edit -json | jq -e '[(.Replace // [])[] | select(.Old.Path | startswith("go.vipengele.org/") | not)] | length == 0'   # no replace of a non-sibling
go mod tidy && test -z "$(git status --porcelain -- go.mod go.sum)"   # tidy leaves no diff
go build ./...
# only when the project holds a *_windows.go file, in any of its packages
GOOS=windows go vet ./...
go vet ./...
go test -race -shuffle=on ./...
```

Every command must pass. Then merge the pull request; the merge is the release.

## 6 — The workflow validates and tags

`auto-release.yml` runs on the push of the merge to `main`, plans the pending projects (notes exist,
tag does not) as dependency tiers and calls `release.yml` for each. The Worker check is part of its
validation: `go.vipengele.org` is a Cloudflare Worker whose module list `publish-vanity-modules.yml`
writes from `source/**/go.mod` when a `go.mod` merges to `main`, and validation retries
`https://go.vipengele.org/<project>?go-get=1` for up to 5 minutes before tagging. A project added
together with its notes waits for that workflow. A tag is final: the module proxy and the checksum
database keep it permanently, so a wrong release is superseded by a new version, never moved.

## 7 — Watch it

```bash
gh run list --workflow auto-release --limit 3
gh run watch <run-id>
```

`release.yml` has three jobs: `verify` (read-only; validates the version, the project, the notes and
the commit, which is on `main` or a release commit whose single parent is, that the project's
`go.mod`, once the release commit has dropped the `replace`s and tidied against the tags, carries no
`replace` and requires only tagged siblings, and the Worker answer, then runs with `GOWORK=off`
`go mod tidy` with no diff, `go build`, `GOOS=windows go vet` when the project holds a
`*_windows.go` file, `go vet` and the module's suite) on the release commit, `tag` (write; rebuilds
the identical release commit, requires the id that `verify` tested, creates the tag and pushes only
the tag) and `announce` (write; asks the module proxy for the version, then creates the GitHub
Release).

## 8 — Finish a partial release

A failure rolls nothing back. A later push to `main` sweeps again and finishes what is pending or
unannounced. To finish one at once, dispatch `release.yml` with the existing tag:

```bash
gh workflow run release.yml -f tag=source/<project>/v<version>
```

A tag pushed by hand runs nothing.

## 9 — Verify

```bash
GOWORK=off GOFLAGS=-mod=mod go list -m go.vipengele.org/<project>@v<version>
gh release view source/<project>/v<version>
```

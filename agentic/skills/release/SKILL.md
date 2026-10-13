---
name: release
description: >-
  Use this skill when the user asks to release, publish, cut or tag a version of
  a project in this repo (e.g. "release cli 0.2.0", "cut a patch of cli").
  Covers choosing the version, the release order when projects depend on each
  other, writing the release notes, tagging, and verifying the release.
---

# Release a project

A release is one tag for one project: `source/<project>/vX.Y.Z`. It releases that project alone
(ADR-0001). `<project>` is the project's directory relative to `source/` and may contain slashes
(`cli/core`). The module path is `go.vipengele.org/<project>`; the tag carries `source/` because it
names the repository directory. The tag is the publication — the Go module proxy serves the module
from it, resolving the path through `go.vipengele.org` — so there is no upload step and no
credential.

There is no release tooling that bumps files. The tag is the only input, and a human pushes it.

## 1 — Decide the version

Read `git log` for `source/<project>/` since the project's last tag
(`git tag --list 'source/<project>/v*' --sort=-v:refname | head -1`) and pick the bump from the
Conventional Commit types: a `!` or `BREAKING CHANGE` is major, `feat` is minor, anything else is
patch. Before `v1.0.0` a breaking change is a minor bump.

A major version of `v2` or above changes the module path (`agentic/rules/module-path-matches-the-vanity-map.md`).
Stop and confirm with the user before releasing one.

## 2 — Release dependencies first, at release time only

If the project requires siblings (`go list -m all | grep go.vipengele.org/`), its release needs each
of them released at a version that contains what the project uses. Release first, with this same
skill, every sibling whose latest release lacks something the project uses. The order matters only
here: no sibling is ever released so that a dependent can be developed, and a project nothing
unreleased feeds — a patch, or a project no sibling uses — is released on its own whenever it is
ready.

## 3 — Write the release notes

`docs/release-notes/<project>/v<version>.md`. **The workflow fails without it**, before it
announces anything. Say what changed and what a consumer has to do about it. A breaking change names
the old and new spelling; "various fixes" is not release notes.

The notes put the project into strict mode until its tag exists: `verify-sibling-deps` then fails
any `replace` of a sibling in its `go.mod` and any sibling version with no tag, on every change.

## 4 — Require the released siblings in the same change

A sibling developed together with the project is replaced by its directory
(`agentic/rules/a-sibling-is-developed-by-a-directory-replace.md`). Drop each `replace` and require
the sibling at its tag:

```bash
cd source/<project>
go mod edit -dropreplace=go.vipengele.org/<sibling>
go get go.vipengele.org/<sibling>@v<sibling-version>
go mod tidy
```

The notes, the dropped `replace`s, the raised `require`s and the tidied `go.sum` are one release
pull request, `chore(<project>): release <version>`. Its `GOWORK=off` CI compiles the project
against the released siblings, as a consumer of the tag will. Nothing is released on merge.

## 5 — Check it locally, merge it, and check the merge commit again

This check is mandatory, on the release branch before merging and on the merge commit before the
tag. A `GOWORK=off` build honours the project's own `replace`, so only reading `go.mod` catches a
leftover one, and a pushed tag is permanent:

```bash
cd source/<project>
export GOWORK=off
go mod edit -json | jq -e '(.Replace // []) | length == 0'   # no replace left
go mod tidy && test -z "$(git status --porcelain -- go.mod go.sum)"   # tidy leaves no diff
go build ./...
go vet ./...
go test -race -shuffle=on ./...
```

Every command must pass. Then merge the pull request.

## 6 — Deploy the Worker with the project's path

`go.vipengele.org` is a Cloudflare Worker whose module list is generated from `source/**/go.mod`. A
project resolves only after the Worker deploy that includes its `go.mod`, so the deploy comes before
the tag. The deploy is a tag of its own: push the next `tools/vanity-worker/vX.Y.Z` at a commit on
`main` that contains the project's `go.mod`, which runs `deploy-vanity-worker.yml`. Then check what
the Worker answers:

```bash
git tag tools/vanity-worker/v<version> origin/main
git push origin tools/vanity-worker/v<version>
curl -s 'https://go.vipengele.org/<project>?go-get=1'
```

The answer must carry a `go-import` meta tag naming the project
(`go.vipengele.org/<project> git https://github.com/vipengele/go source/<project>`). Until it does,
push no tag.

## 7 — Tag the merged commit

```bash
git fetch origin
git checkout --detach <merge-commit>   # run the step 5 checks here
git tag source/<project>/v<version> <merge-commit>
git push origin source/<project>/v<version>
```

The tag must point at the release pull request's merge commit on `main`, not at the branch, and
only after the step 5 checks pass there. A pushed tag is final: the module proxy and the checksum
database keep it permanently, so a wrong tag is superseded by a new version, never moved. A tag
pushed while the Worker does not answer is permanent too: `announce` fails, and once the Worker
answers, re-running the workflow finishes the release. If the tag itself was wrong, the next attempt
is a new version.

## 8 — Watch it

```bash
gh run list --workflow release --limit 3
gh run watch <run-id>
```

The jobs are `tag` (validates the tag, the project, the commit and the notes, and that the tagged
project's `go.mod` carries no `replace` and requires only tagged siblings), `test` (with
`GOWORK=off`, `go mod tidy` with no diff, `go build`, `go vet` and the module's suite) and
`announce` (asks the module proxy for the version, then creates the GitHub Release). Re-running
finishes a partial release.

## 9 — Verify

```bash
GOWORK=off GOFLAGS=-mod=mod go list -m go.vipengele.org/<project>@v<version>
gh release view source/<project>/v<version>
```

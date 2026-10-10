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
(ADR-0001). The tag is the publication — the Go module proxy serves the module from it — so there
is no upload step and no credential.

There is no release tooling that bumps files. The tag is the only input, and a human pushes it.

## 1 — Decide the version

Read `git log` for `source/<project>/` since the project's last tag
(`git tag --list 'source/<project>/v*' --sort=-v:refname | head -1`) and pick the bump from the
Conventional Commit types: a `!` or `BREAKING CHANGE` is major, `feat` is minor, anything else is
patch. Before `v1.0.0` a breaking change is a minor bump.

A major version of `v2` or above changes the module path (`agentic/rules/module-path-matches-its-directory.md`).
Stop and confirm with the user before releasing one.

## 2 — Release dependencies first

If the project requires siblings (`go list -m all | grep github.com/vipengele/go/source/`), and
those siblings have changes the project needs, release each sibling first, then raise this
project's requirement in a change of its own:

```bash
cd source/<project>
go get github.com/vipengele/go/source/<sibling>@v<sibling-version>
go mod tidy
```

`verify-sibling-deps` refuses a sibling version that has no tag, so the order is enforced.

## 3 — Write the release notes

`docs/release-notes/<project>/v<version>.md`. **The workflow fails without it**, before it
announces anything. Say what changed and what a consumer has to do about it. A breaking change names
the old and new spelling; "various fixes" is not release notes.

## 4 — Open it as a pull request, and merge it

The notes and any `require` raise are an ordinary change: `chore(<project>): release <version>` as
the subject. Nothing is released on merge.

## 5 — Tag the merged commit

```bash
git fetch origin
git tag source/<project>/v<version> origin/main
git push origin source/<project>/v<version>
```

The tag must point at the merged commit on `main`, not at the branch. A pushed tag is final: the
module proxy caches it permanently, so a wrong tag is superseded by a new version, never moved.

## 6 — Watch it

```bash
gh run list --workflow release --limit 3
gh run watch <run-id>
```

The jobs are `tag` (validates the tag, the project, the commit and the notes), `test` (the module's
suite with `GOWORK=off`) and `announce` (asks the module proxy for the version, then creates the
GitHub Release). Re-running finishes a partial release.

## 7 — Verify

```bash
GOWORK=off GOFLAGS=-mod=mod go list -m github.com/vipengele/go/source/<project>@v<version>
gh release view source/<project>/v<version>
```

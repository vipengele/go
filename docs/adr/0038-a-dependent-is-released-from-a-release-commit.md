# A dependent is released from a release commit

Status: accepted. It amends the release pull request, strict-mode and "one pull request per tier"
passages of
`0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md` and the rule of
`0037-a-release-is-the-merge-of-its-notes.md` that a dependency chain needs one pull request per
tier.

A project that requires a sibling is released from the same pull request as the sibling. The pull
request adds the notes of both. The dependent keeps its directory `replace` on `main` and requires
the sibling at the sibling's pending version. When `auto-release.yml` has tagged and announced the
sibling, the dependent's release builds a **release commit** and tags it. The release commit is the
merge commit with only the dependent's `go.mod` and `go.sum` changed: the `replace` is dropped and
the module tidied against the sibling's new tag. Only the tag is pushed; `main` is untouched.

## The rules

- **The dependent keeps its `replace` on `main`.** The release pull request adds the notes and
  requires each sibling at its pending version (notes exist, tag does not) or at an existing tag.
  It drops nothing.
- **Strict mode accepts a directory `replace`.** While a project's notes have no tag, a sibling may
  stay replaced by its own directory, with no version on either side, if the required version is the
  sibling's pending version or an existing tag of the sibling. A sibling required without a
  `replace` is tagged. A `replace` naming a version or another directory, a sibling at a version
  that is neither pending nor tagged, and a pseudo-version fail. The check is `verify.sh` in
  `verify-sibling-deps`.
- **A tagged `go.mod` carries no `replace` because the release commit makes it so.** `verify` in
  `release.yml` (read-only) runs `release-project.sh prepare`: it drops the dependent's sibling
  `replace`s, runs `go mod tidy` against the sibling's tag, pushed and announced by an earlier tier of
  `auto-release.yml`, and builds the release commit. It validates and tests that commit and uploads
  its `go.mod` and `go.sum` as an artifact.
- **The commit is deterministic.** The parent is the merge commit `sha`, the tree differs from it in
  `source/<project>/go.mod` and `go.sum` only, the subject is `chore(release): <project> <version>`,
  and author and committer are `github-actions[bot]` at the commit time of `sha`.
- **`tag` rebuilds the identical commit.** `tag` (write; runs no project code) takes the artifact,
  builds the commit on `sha` the same way, requires the same commit id as the one `verify` tested,
  creates the annotated tag on it and pushes only the tag.
- **Validation accepts a release commit.** The commit to release is on `main`, or has a single parent
  on `main` and a diff against that parent that touches only the project's `go.mod` and `go.sum`.
- **A chain stopped halfway is repaired forward.** If tier 1 is tagged and the dependent fails, `main`
  still passes repository-wide CI, because the dependent is in strict mode with a `replace` and a
  tagged sibling. The fix is an ordinary pull request, and the next sweep releases what is pending.

## Considered options

- **Pushing the release commit to `main`.** The `main` ruleset requires a pull request, and the
  workflow holds no credential that bypasses it. A release commit on `main` would also make the
  workflow a second author of `main`, racing the merges it follows.
- **One pull request per tier.** A sibling is tagged by a merge before the
  dependent's pull request can drop its `replace`, so a chain of n projects costs n reviewed
  pull requests, each of which restates the notes of the one before. A change that spans two
  projects is one pull request (ADR-0006), and its release should be too.
- **Dropping the `replace` on `main` in the release pull request.** The pull request's CI would
  then need the sibling's tag, which does not exist before the merge, so strict mode would still
  fail every dependent of an unreleased sibling.

## Consequences

- **The commit id is checked across jobs.** `verify` and `tag` build the release commit from the same
  inputs and the commit is deterministic, so `tag` proves that the commit it tags is the one that was
  validated and tested.
- **The release commit is on no branch.** Only its tag reaches it, and its parent is a commit on
  `main`. `git log` of `main` never shows it.
- **The `go.sum` of the tagged tree differs from `main`'s.** The tag's `go.mod` requires the sibling at
  its tag with no `replace`, so what a consumer builds is not byte-identical to what `main` holds for
  the same version. `GOWORK=off` CI on `main` builds the dependent through the `replace`; the release
  commit is built and tested by `verify` against the real tag.
- **A dry run of a chain fails at the dependent.** A dry run tags nothing, so the sibling of tier 1 has
  no tag for the dependent's `go mod tidy` to resolve.
- **Strict mode does not prove that a `go.mod` on `main` has no `replace`.** The release commit
  guarantees it for every tagged `go.mod`.

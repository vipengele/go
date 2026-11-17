# A release is the merge of its notes

Status: accepted. It supersedes the rule of
`0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md` that a human pushes the tag,
and amends the release pull request and `tag` job paragraphs of
`0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md`. It leaves the
permission seam of `0033-a-tool-releases-through-the-update-release-workflow-pinned-at-the-update-tag.md`
as the ground for how the release jobs hold their tokens.

A project is released when a pull request that adds `docs/release-notes/<path>/vX.Y.Z.md` merges to
`main`. The workflow `auto-release.yml` then tags the project and releases it. The tag form stays
`source/<path>/vX.Y.Z` and the release stays one project and one tag (ADR-0001); what changes is who
creates the tag: the workflow, from the merge commit, and no person.

## The rules

- **Merging the notes is the release.** The only human act is merging a pull request that adds the
  notes of one or more projects. `auto-release.yml` runs on every push to `main`, and tags and
  releases every pending project: one whose notes exist and whose tag does not.
- **Tags are unsigned annotated tags** made with `GITHUB_TOKEN` as `github-actions[bot]`. The tag
  message carries the URL of the run that made it.
- **`release.yml` has no tag-push trigger.** A tag pushed by hand runs nothing. A tag pushed with
  `GITHUB_TOKEN` starts no workflow either (ADR-0016), so the release is a reusable workflow that
  `auto-release.yml` calls, not one a tag triggers. A `workflow_dispatch` with the tag finishes a
  partial release.
- **Every run sweeps.** A run releases every pending project and announces every unannounced one: a
  tag whose commit carries its notes and that has no GitHub Release. A failure rolls nothing back.
  The next push retries from its own commit, and a re-run retries at the original commit.
- **Tiers of reusable-workflow calls.** A read-only `plan` job emits the pending set as dependency
  tiers, ordered by the sibling edges between projects. Fixed jobs `tier-1` to `tier-8` each call
  `release.yml` as a matrix over their tier. A tier job needs the previous one and runs whenever that
  job was not cancelled. `plan` fails when the graph is deeper than 8. A dependent of a failed
  project fails its own validation, because its sibling's tag is missing, so no list of skipped
  projects is kept. Each matrix entry runs `setup-go` from its own project's `go.mod`.
- **The permission seam stays.** In `release.yml`, `verify` holds `contents: read`, checks out with
  `persist-credentials: false`, validates and runs the project's tests. `tag` and `announce` hold
  `contents: write` and run no project code.
- **The Worker answers before the tag.** Validation retries
  `https://go.vipengele.org/<path>?go-get=1` for up to 5 minutes. A pull request that adds a project
  together with its notes waits for `publish-vanity-modules.yml` (ADR-0016) instead of tagging a
  module the proxy cannot resolve.
- **Concurrency.** `auto-release` uses one group without cancelling, and a run cancelled while queued
  is harmless because the next run sweeps. Inside `release.yml` the group is keyed per tag,
  `release-<tag>`, so a dispatch that finishes a release and a sweep never announce one tag at once.
  `gh release create` failing with "already exists" counts as success.
- **A tag that exists at release time** was made by a concurrent run. In release mode a rejected tag
  push fails that entry, and the next sweep no longer sees the project as pending.
- **The release self-test gates merges.** `ci-orchestration.yml` calls `release-selftest.yml`
  unconditionally and `ci-gate` needs it. It is offline and quick.
- **Edge cases.** More than one pending version for a project fails `plan`. Notes without a project,
  and tags without notes such as `source/spike/probe/v0.0.1`, are ignored.
- **A dependency chain needs one pull request per tier.** A project that requires a sibling with no
  tag still fails validation, so a dependent is released by a pull request after its sibling's tag
  exists.

## Considered options

- **A manual tag push kept as a fallback.** A trigger on tag push would keep a second path to a
  release that skips the sweep, and a hand-pushed tag would run against whatever commit its author
  chose. One path, the merge of the notes, is the one a reviewer approved. A partial release is
  finished by dispatching `release.yml`, which validates at the tag's commit.
- **Tags signed in CI.** A signing key in the workflow is a publication credential the repository
  does not otherwise hold (ADR-0001: the tag is the publication), and the signature would say that
  the CI ran, which the annotated tag's message and the bot identity already say. Unsigned
  annotated tags are enough, and a tag ruleset on `source/*` is repository hardening a human applies.
- **A single sequential job holding a write token beside code under test.** A job that runs a
  project's tests and then pushes a tag has the token in reach of that code. ADR-0033 rejected a
  workflow that holds a write token with code it does not control, and the `verify`, `tag` and
  `announce` seam is the safety property here. One job also cannot run `setup-go` once per project,
  because `setup-go` sets `GOTOOLCHAIN=local`, so each project needs its own toolchain from its own
  `go.mod`.
- **A `workflow_call` per tag with no tiers.** Calls made in parallel for a project and its sibling
  race: the dependent validates before the sibling's tag exists. Tiers give the order, a `needs`
  chain between them, and a matrix inside each.

## Consequences

- **Merging the notes cannot be undone by the workflow.** A tag is permanent, so a notes file that
  merges releases the project. The review of the notes is the review of the release.
- **A pull request with notes for a project whose sibling is unreleased fails the release, not the
  merge.** The merge succeeds and `auto-release` fails that project's validation; the fix is an
  ordinary pull request after the sibling's tag exists.
- **Every push to `main` runs `plan`.** A push that adds no notes finds nothing pending and
  releases nothing.
- **A failed announce is retried by the next sweep.** The tag exists, its commit carries the notes
  and there is no GitHub Release, so the project is unannounced and the next run announces it.

---
about: "facts behind auto-release: ADR-0037 supersedes ADR-0001's human tag; tags and dispatches made with GITHUB_TOKEN trigger nothing (so release.yml is reusable, no tag trigger); no tag ruleset is in git; one PR releases a dependency chain: auto-release tiers it and a dependent is tagged on a release commit that drops its sibling replace (ADR-0038)"
saw:
  - .github/workflows/release.yml
  - .github/workflows/auto-release.yml
  - .github/scripts/release/pending.sh
  - .github/scripts/release/release-project.sh
  - docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md
  - docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md
  - docs/adr/0016-the-vanity-module-list-is-published-as-data-to-r2-from-main.md
  - docs/adr/0033-a-tool-releases-through-the-update-release-workflow-pinned-at-the-update-tag.md
  - docs/adr/0037-a-release-is-the-merge-of-its-notes.md
  - .github/workflows/deploy-vanity-worker.yml
  - .github/actions/verify-sibling-deps/action.yml
  - .github/actions/changed-projects/action.yml
  - .github/actions/verify-sibling-deps/verify.sh
  - docs/adr/0038-a-dependent-is-released-from-a-release-commit.md
---

Re-checked 2026-11-17. The original exploration (issue 211, auto-release from main) was answered by
ADR-0037 and the branch implementing it. What holds now:

- ADR-0001 still says "A human pushes the tag" (`0001-...:26`); ADR-0037 (`:1-7`) records that it
  supersedes that rule. A tag pushed by hand now runs nothing (`release.yml:3-9`).
- Why the release is a called workflow: a tag or dispatch made with GITHUB_TOKEN triggers no other
  workflow and the repo has no GitHub App or PAT (`0016-the-vanity-...:49-51`, repeated at
  `release.yml:5-7` and `auto-release.yml:21-22`). `auto-release.yml` therefore calls
  `release.yml` per project (tier jobs `tier-1`..`tier-8`) instead of relying on a tag trigger.
- release.yml: `workflow_call` (project, version, sha, mode release|finish, dry_run) plus
  `workflow_dispatch` with `tag` for finishing (`release.yml:33-68`). Concurrency group is per tag
  (`:77-79`), auto-release's is one sweep at a time, cancel-in-progress false (`auto-release.yml:45-47`).
  `verify` holds a read-only token and is the only job running project code; `tag` and `announce`
  hold contents: write and run none (`release.yml:18-25`, `:214-221`, `:240-251`).
- Precedent of a tag-input dispatch: `deploy-vanity-worker.yml:33-37`, `:64-67`.
- Ordering: `pending.sh` emits dependency tiers (`pending.sh:29-37`), fails above 8 tiers, on cycles,
  and on more than one pending version of a project. One PR may carry a whole chain (ADR-0038, which
  replaces ADR-0037:55's "one pull request per tier"): the dependent keeps its directory replace on
  main and requires the sibling at its pending version, which strict mode accepts
  (`verify-sibling-deps/verify.sh:138`). When its tier runs, `verify` runs
  `release-project.sh prepare` (`cmd_prepare`, `:455`): it drops the sibling replaces, tidies against
  the sibling's tag pushed by the earlier tier, and builds a deterministic release commit
  (`build_release_commit`, `:234`: parent `sha`, only `source/<p>/go.mod`/`go.sum` changed, subject
  `chore(release): <p> <v>`, bot identity, `sha`'s commit time). `tag` rebuilds it from the uploaded
  go.mod/go.sum, requires the same id (`:549`) and pushes only the tag; the commit is on no branch.
  `on_main` (`:214`) accepts a commit on main or a release commit whose single parent is. A dry run
  of a chain fails at the dependent, since tier 1 tags nothing.
- Tags are unsigned annotated tags by `github-actions[bot]` (`release-project.sh:337-357`); ADR-0037
  rejects signing in CI. Nothing verifies tag signatures.
- No tag ruleset: ADR-0037:65 calls `source/*` tag protection "repository hardening a human applies"
  and ADR-0033:93 likewise. It is live repo settings, not in git; the earlier check
  (tags/protection 404, one branch ruleset `gt` on main) was not repeated.
- `changed-projects` (`action.yml:124-143`) builds edges from `go mod edit -json` requires; its
  output is a JSON list with no topological order, so it was not reused for tiers; `pending.sh`
  has its own owner jq (`lib.sh` `RELEASE_JQ_OWNER`).

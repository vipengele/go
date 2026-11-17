---
about: "facts behind auto-release: ADR-0037 supersedes ADR-0001's human tag; tags and dispatches made with GITHUB_TOKEN trigger nothing (so release.yml is reusable, no tag trigger); no tag ruleset is in git; a dependency chain needs one PR per tier because strict mode and validate require tagged siblings"
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
  and on more than one pending version of a project. It orders pending projects only; it does not
  let a dependent release in the same PR as an untagged sibling: "A dependency chain needs one pull
  request per tier" (ADR-0037:52-54), because `validate` requires each sibling's tag on origin
  (`release-project.sh:254-256`) and `verify-sibling-deps` strict mode demands tagged siblings in the
  PR's CI (`verify-sibling-deps/action.yml:109-125`). Not re-run: how the tiers behave when two
  tiers' notes land in one PR (ADR-0037 says it fails validation).
- Tags are unsigned annotated tags by `github-actions[bot]` (`release-project.sh:337-357`); ADR-0037
  rejects signing in CI. Nothing verifies tag signatures.
- No tag ruleset: ADR-0037:65 calls `source/*` tag protection "repository hardening a human applies"
  and ADR-0033:93 likewise. It is live repo settings, not in git; the earlier check
  (tags/protection 404, one branch ruleset `gt` on main) was not repeated.
- `changed-projects` (`action.yml:124-143`) builds edges from `go mod edit -json` requires; its
  output is a JSON list with no topological order, so it was not reused for tiers; `pending.sh`
  has its own owner jq (`lib.sh` `RELEASE_JQ_OWNER`).

---
about: "the tag no longer exists before validation: release.yml has no tag trigger, verify validates and tests, then tag pushes the tag"
saw:
  - .github/workflows/release.yml
  - .github/workflows/auto-release.yml
  - .github/scripts/release/release-project.sh
  - .github/scripts/release/lib.sh
  - docs/adr/0037-a-release-is-the-merge-of-its-notes.md
targets: release-tag-exists-before-validation
verdict: now-false
---

Re-checked 2026-11-17 against the current files. The note's claim (tag pushed by a human triggers release.yml, so the checks run after the tag is on the remote; notes merged afterwards cannot repair it because checkout is the tagged commit) is false. It also supersedes the still-true candidates 20261017-release-tag-exists-before-validation-recheck.md, 20261116-release-prerelease-note-now-false.md (its last sentence) and 20261017-release-tag-job-order-and-sibling-state.md / 20261116-release-automation-constraints.md on the trigger and the job order.

Current flow:
- `release.yml:33-68` triggers are `workflow_call` (inputs project, version, sha, mode `release|finish`, dry_run) and `workflow_dispatch` (inputs `tag`, `dry_run`). No `push: tags`. Header `:3-9` says a tag pushed by hand releases nothing (GITHUB_TOKEN tags start no workflow, ADR-0016).
- `auto-release.yml:305-308` runs on `push: branches: [main]` plus dispatch; `plan` job runs `pending.sh` (pending = notes file at the pushed commit with no tag), tiers 1-8 call release.yml with `mode: release`, `sha: github.sha` (`:437-452`); `finish` job (`:577-591`) calls it with `mode: finish` for each tag with no GitHub Release.
- Order in release.yml: `verify` (`:85-212`, read-only token) -> `tag` (`:214-238`, `contents: write`, `if mode == release && !dry_run`) -> `announce` (`:240-278`). `verify` runs `release-project.sh validate` (`:201-202`) then `test` (`:208-212`, the last step); only then does `tag` run `release-project.sh tag` (`cmd_tag`, script `:313-334`) which creates an unsigned annotated tag by the bot identity at the validated sha and pushes only that ref.
- Validation happens at the sha, before any tag: notes exist at the commit (script `:185-187`), commit on origin/main, go.mod has no replace and siblings at tagged versions, and in mode `release` the tag must NOT exist on origin (`:245-248`, via `git ls-remote --tags origin`, `:222`). In mode `finish` the tag must exist and point at the checked-out commit (`:250-253`). Then `worker_answers` requires `https://go.vipengele.org/<project>?go-get=1` to answer (`:257-258`) - so a Worker that does not answer now fails in `verify`, before the tag is pushed. `dry_run` runs `verify` alone.
- Partial release: a tag pushed but not announced is finished by the next auto-release sweep (`finish` job) or by `workflow_dispatch` of release.yml with `tag: source/<project>/vX.Y.Z` (mode empty -> `finish`, `release.yml:114-117`); the checkout is `refs/tags/<tag>` (`:151`). Announce (`release-project.sh announce`, `:335-`) is idempotent: an existing GitHub Release counts as success (`:358-360`, `:376-378`).
- Scripts always come from `github.sha` of the workflow run, not from the released commit (`release.yml:27-30`, `:163-169`).
- Concurrency group `release-<tag>`, no cancel (`release.yml:77-79`).

What still holds from the note: notes must be merged before release (the notes file at the commit is a validation); this is now structural, since the release is triggered by the merge of the notes (ADR-0037). Nothing needs "tag from merged main" by hand.

Not re-checked: ADR-0037 text itself was only skimmed via the workflow headers; the live repo settings (no tag ruleset) are outside git.

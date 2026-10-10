---
about: "the release tag exists before validation runs, and the job checks out the tagged commit, so missing notes cannot be fixed by re-running"
saw:
  - .github/workflows/release.yml
  - agentic/skills/release/SKILL.md
---

Kind: gotcha.

The header comment (`release.yml:50-53`) says everything that can fail "without leaving anything half-released" fails in the `tag` job. But the workflow is triggered by the tag push (`:18-22`), so the tag already exists on the remote when `tag` fails (missing `docs/release-notes/<project>/<version>.md` at `:75-79`, commit not on main at `:70`, project missing at `:65`).

`actions/checkout` at `:45` checks out the tagged commit. Re-running the workflow after merging the notes to main therefore still fails: the notes are absent at the tagged commit. The skill (SKILL.md step 5) says the tag must be created from merged origin/main after notes are merged (step 4), which is the only way to avoid this. The skill states "never move a tag" because the module proxy caches permanently. A tag that failed in `tag`/`test` never reached the proxy (only `announce` asks it), so it can in practice be deleted and re-pushed, but once `announce` has run it must be superseded by a new version.

"Re-running finishes a partial release" (CLAUDE.md, SKILL.md step 6) is true only for failures after the tag job, e.g. proxy lag or the gh release step (idempotent, `:154-157`).

What breaks: tagging before the notes PR is merged yields a red run that no re-run can fix.

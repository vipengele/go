---
about: "release-flow doc drift is only in rendered output; the sources (agentic/vipengele-go.md, agentic/skills/release/SKILL.md, agentic/rules/a-sibling-*.md, CONTEXT.md, README.md) already describe ADR-0037/0038"
saw:
  - agentic/vipengele-go.md
  - agentic/skills/release/SKILL.md
  - agentic/rules/a-sibling-is-developed-by-a-directory-replace.md
  - CLAUDE.md
  - .claude/skills/release/SKILL.md
  - .claude/rules/a-sibling-is-developed-by-a-directory-replace.md
  - docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md
  - docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md
  - docs/adr/0037-a-release-is-the-merge-of-its-notes.md
  - CONTEXT.md
  - README.md
---

Checked 2026-11-17 with `diff` between rendered and source files. CLAUDE.md is rendered from the
manifest's `context: ./agentic/vipengele-go.md`; `.claude/skills/release/SKILL.md` from
`agentic/skills/release/SKILL.md`; `.claude/rules/*` from `agentic/rules/*`. All three renders
(dated Nov 11) still say "pushed by a human", "drops the sibling replaces", "dependency-first"
(CLAUDE.md Release section), while the sources (vipengele-go.md:116-140, SKILL.md:19-21, rule :35-65)
already describe auto-release and the release commit. Fix is `agtk render`, not an edit.
The ADR bodies agree with the sources: ADR-0001 (Decision, "auto-release workflow makes the tag"),
ADR-0006 (Release, Why, Consequences: tiers from one pull request, the release commit drops the
`replace`, `release.yml`'s `verify`/`tag`/`announce`) and ADR-0037 (one pull request per chain).
Only their status headers still name the hand-pushed tag, as the record of what was amended.
`docs/guide/consumer.md:375` "Push the tag" is about a tool built on `update`, not this repo: not stale.
Supplements 20261117-multi-project-release-is-notes-plus-require-bump (whose "Doc drift" paragraph
described the then-unrendered sources).

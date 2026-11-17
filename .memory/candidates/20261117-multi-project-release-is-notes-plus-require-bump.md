---
about: "a cross-project release PR is notes + dependent require bumped to sibling's pending version; no human tag; a notes-only follow-up PR works only if the require already names the pending/tagged version"
saw:
  - docs/adr/0037-a-release-is-the-merge-of-its-notes.md
  - docs/adr/0038-a-dependent-is-released-from-a-release-commit.md
  - .github/workflows/auto-release.yml
  - .github/workflows/release.yml
  - .github/actions/verify-sibling-deps/verify.sh
  - .github/scripts/release/release-project.sh
  - .github/scripts/release/pending.sh
---
Flow (ADR-0037:11-14, 0038:9-14): merging a PR that adds docs/release-notes/<p>/vX.Y.Z.md is the release;
auto-release.yml (push to main, :27-30) runs pending.sh (:110-114), tiers by sibling `require` edges
(lib.sh release_edges :69-83; pending.sh :30-35), calls release.yml per tier (:159+). No human tag:
release.yml has only workflow_call/workflow_dispatch(finish) (:39-74); `tag` job pushes it (:198-239).

Dependent (core, update) keeps its directory replace on main and must REQUIRE the sibling at its
pending version (verify.sh:127-149, strict branch :137; ADR-0038:18-26). So the PR = 3 notes files
+ `require go.vipengele.org/cli/spec v0.2.0` in cli/core and update go.mod (not the v0.1.0/v0.0.0
placeholder). Notes alone are not enough: at v0.0.0 verify-sibling-deps fails (no tag, no notes);
at last-released v0.1.0 it passes (tagged) but `release-project.sh prepare` tidies against tag v0.1.0
(:46-63) and the build against the old API fails in `verify`. Tiering: spec tier 1, core/update tier 2.
Release commit = merge sha + only go.mod/go.sum changed, replace dropped (release-project.sh:190-261).

Feature PR without notes + leftover replace on main: allowed. No notes => development mode; verify.sh
accepts a version-less replace by the sibling's own dir (:99-125), required version never looked up
(:127-130). A later notes-only PR works only if the require already is the pending/tagged version.

Doc drift: CLAUDE.md Release section, .claude/skills/release/SKILL.md (:15 "a human pushes it", :60
"Nothing is released on merge", :45-56 drop replace, :97-104 tag push, :120-124 old job names tag/test/announce)
and .claude/rules/a-sibling-is-developed-by-a-directory-replace.md (strict mode fails any replace) still
describe the pre-ADR-0037/0038 flow. ADR-0006 header (:1-9) already says it is amended by 0037/0038.
ADR-0037/0038 + the scripts are authoritative. ADR-0037:55-57 (one PR per tier) is itself superseded by 0038.
Two ADRs are numbered 0037 (also 0037-config-collections-...).

---
about: "verify-sibling-deps (verify.sh) has a development mode (version-less directory replace of the sibling accepted at any version) and a state-based strict mode (release notes without a tag: a directory replace only at the sibling's pending or tagged version, unreplaced siblings tagged); tags are listed once, unauthenticated, and a failed listing stops the check"
saw:
  - .github/actions/verify-sibling-deps/action.yml
  - .github/actions/verify-sibling-deps/verify.sh
  - .github/scripts/release/test/run.sh
  - docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md
  - docs/adr/0038-a-dependent-is-released-from-a-release-commit.md
targets: verify-sibling-deps-tag-check-unauthenticated
verdict: now-false
---

The note states a rule that does not hold: a `replace` of a sibling is always rejected, a sibling `require` is always looked up as a tag, and a failed `git ls-remote` reads as "no tag". ADR-0006 and ADR-0038 replace it.

The check is `.github/actions/verify-sibling-deps/verify.sh <root> <remote>`; `action.yml` only calls it, and `.github/scripts/release/test/run.sh` (the `verify-sibling-deps:` cases) tests it against fixture repositories. It checks every `source/**/go.mod`, per project, in one of two modes read from repo files and tags, never from a diff.

- Strict (`verify.sh:95`): some `docs/release-notes/<project>/v<version>.md` (direct children only) has no tag `source/<project>/v<version>`. A sibling may keep its directory replace when it is required at the sibling's pending version (`is_pending`, `:45`: its notes exist, its tag does not) or at a tagged one (`:138`); a sibling without a replace must be tagged (`:143`). This lets one PR carry a dependency chain; the release commit (ADR-0038, `release-project.sh` `cmd_prepare`) drops the replace before the tag.
- Development: a replaced sibling's required version is never looked up, so it may be `v0.0.0`; without a replace it must have the tag `source/<sibling>/<version>`.
- Both modes: a replace is accepted only when `New.Path`, resolved against the go.mod's directory with `realpath -m`, is exactly `source/<sibling>` and neither side names a version (`:111`, `:122`). A sibling at a pseudo-version fails (`:134`), as do a replace of a non-sibling, a module path other than `go.vipengele.org/<dir>` (`:76`) and nested go.mod pairs (`:55`).

Tags come from ONE unauthenticated `git ls-remote --tags --refs <remote>` (`has_tag`, `:26`), cached and made only when a verdict needs it. A failed listing is an `::error::` and `exit 1`, not "no tag".

Strict mode does not prove that a go.mod on main has no replace; the release commit guarantees it for every tagged go.mod (ADR-0038 Consequences). Out of scope, unfixed: a `/vN` sibling path (`go.vipengele.org/a/v2`) is looked up as project `a/v2`, while `lib.sh` maps it to `a`.

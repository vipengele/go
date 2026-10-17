---
about: "release.yml tag job order (regex, go.mod exists, no nesting, tag on main, notes file, then go.mod consumer check) and day-one sibling state: commons has notes v0.1.0 but no tag (strict, harmless), cli/spec and cli/core replace commons by directory, and the deployed Worker tag v0.1.2 predates cli/core, cli/spec, commons and config"
saw:
  - .github/workflows/release.yml
  - .github/actions/verify-sibling-deps/action.yml
  - source/cli/spec/go.mod
  - source/commons/go.mod
  - docs/release-notes/commons/v0.1.0.md
---

release.yml `tag` job order: (1) step `parse` (`:57-104`): tag regex `:61` (accepts `-suffix`), `source/<p>/go.mod` exists `:68`, no enclosing or enclosed go.mod `:73-89`, commit is ancestor of origin/main `:91`, notes file `docs/release-notes/<p>/<v>.md` exists `:96`; (2) setup-go from the project's go.mod `:106`; (3) step "The tagged go.mod is what a consumer gets" (`:121-183`): prerelease suffix rejected `:131`, module path equals `go.vipengele.org/<p>` `:141`, any replace fails `:148-155`, then one unauthenticated `git ls-remote` `:160` and each `go.vipengele.org/` require must be a tag `source/<sib>/<ver>` (pseudo-versions fail `:174`). `test` (`:185-216`) then `announce` (`:218-272`): `go list -m` via proxy.golang.org with 6 attempts `:245`; failure message at `:250` tells the human to push a Worker tag and re-run. The `tag` job does not call the verify-sibling-deps action.

verify-sibling-deps action: per project, strict iff some direct-child `docs/release-notes/<p>/v*.md` has no tag (`action.yml:96-113`); strict: any sibling replace fails (`:124-128`) and every sibling require must be tagged (`:151-159`); development: version-less replace to exactly `source/<sib>` (`:129-143`). Both: pseudo-version fails (`:148`), non-sibling replace fails (`:117`).

State at 2026-10-17 (`git ls-remote --tags origin`): only `source/spike/probe/v0.0.1` and `tools/vanity-worker/v0.1.0..v0.1.2`. `docs/release-notes/` holds only `commons/v0.1.0.md`, so commons is strict, and it requires no sibling, so nothing blocks. cli/spec requires `go.vipengele.org/commons v0.0.0` with `replace => ../../commons` (`source/cli/spec/go.mod:5-12`) and has no notes, so it is development mode and passes. Releasing cli/spec means tagging commons first (its notes exist), then in the cli/spec release PR dropping the replace and requiring `commons@v0.1.0`; cli/spec notes then put cli/spec in strict mode until its tag. cli/core replaces both cli/spec and commons (`source/cli/core/go.mod:19-21`), so it releases after both.

Worker staleness: `git merge-base --is-ancestor <commit that added source/X/go.mod> tools/vanity-worker/v0.1.2` is true only for daemon and update; false for cli/core, cli/spec, commons, config. So their module paths resolve only after a Worker tag v0.1.3 or later on a main commit containing them (unverified against the live Worker: the curl was not run).

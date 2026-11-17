---
about: "ci-build runs verify-sibling-deps repo-wide on every PR; the release's verify job runs its own check scoped to the released project (release-project.sh check), so an unreleased sibling elsewhere never blocks a release; a GOWORK=off build alone cannot see a leftover replace"
saw:
  - .github/actions/verify-sibling-deps/action.yml
  - .github/workflows/ci-build.yml
  - .github/workflows/release.yml
  - .github/scripts/release/release-project.sh
  - .github/scripts/release/lib.sh
---

Re-checked 2026-11-17 after the move to auto-release.yml / reusable release.yml (ADR-0037).

Still true:
- `verify-sibling-deps/action.yml` decides per project, strict while a release-notes file has no tag
  (`:96-113`, strict flag `:109-111`) and otherwise development mode; its tag listing is one
  unauthenticated `git ls-remote --tags --refs` (`:51`). `ci-build.yml` calls it on every PR across
  the whole repo. The release path does not call the action.

Changed:
- The scoped check is no longer a step in a `tag` job of a tag-triggered workflow. It is
  `check()` in `release-project.sh:154-285`, run by release.yml's `verify` job
  (`release.yml:191-205`, subcommand `validate` for mode release, `finish-check` for mode finish).
  It reads only `source/<project>/go.mod` from the commit object (`:198-202`), so another project's
  go.mod never blocks the release.
- It fails, reporting all go.mod problems at once (`:262`): module path other than
  `go.vipengele.org/<project>` (with `/vN` for v2+, `:207-218`), any `replace` (sibling or not,
  `:220-228`), a sibling at a pseudo-version (`:251-253`), a sibling version with no tag on origin
  (`:254-256`). Sibling tag names drop a `/vN` suffix (`:238-250`). Tags come from
  `release_remote_tags` (`lib.sh:108-112`), `git ls-remote` on origin, not local refs.
- The prerelease rejection is separate and earlier (`:192-193`).

Why the explicit check is needed (verified earlier in a scratch module pair, not redone): a
`replace => ../sibling` in go.mod is honoured by a `GOWORK=off go build` of the main module, while
every consumer of the tag ignores it; the comment at `:195-197` states it. release.yml sets
`GOWORK: "off"` at workflow level (`release.yml:81-82`). The suite step (`release-project.sh:309-335`,
`cmd_test`) runs `go mod tidy` and fails if go.mod/go.sum change, then build, windows vet when
`*_windows*.go` exist, vet and `go test -race -shuffle=on`. With an unreleased sibling version and no
replace, tidy/build fail, which is the consumer-view proof that the released sibling holds the API
used; the tag-existence check alone does not prove that.

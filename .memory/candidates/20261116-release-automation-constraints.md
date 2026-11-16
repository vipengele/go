---
about: "constraints on automating releases from main: ADR-0001 gives no reason for a human tag; release.yml is push-tag-only and reads github.ref; GITHUB_TOKEN tags do not trigger it; no tag ruleset exists"
saw:
  - .github/workflows/release.yml
  - docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md
  - docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md
  - docs/adr/0016-the-vanity-module-list-is-published-as-data-to-r2-from-main.md
  - .github/workflows/deploy-vanity-worker.yml
  - .github/actions/verify-sibling-deps/action.yml
  - .github/actions/changed-projects/action.yml
---
Explored for issue 211 (auto-release on push to main).

- ADR-0001 (:25) states "A human pushes the tag" as fact; its Considered options (:51-57) are only repo-wide tag, lockstep tags, one root module. No ADR argues for or against automation of the tag. The only rejected CI-tag alternative is ADR-0016:49-51 (CI-created Worker tag: GITHUB_TOKEN tags/dispatches trigger nothing and no App/PAT exists).
- release.yml trigger is only `push: tags: source/**/v[0-9]*...` (:18-24); no workflow_dispatch/workflow_call. Assumes github.ref: GITHUB_REF_NAME in regex (:61), GITHUB_SHA ancestor check (:91), checkout of the triggering ref (default), `gh release view/create "${GITHUB_REF_NAME}"` (:279-285). Global concurrency group `release`, cancel-in-progress false (:31-33). Permissions contents: read, announce contents: write (:26, :238).
- Precedent for tag-input dispatch: deploy-vanity-worker.yml:27-37 (workflow_dispatch input `tag`, resolves GITHUB_REF_NAME vs input, checkout ref refs/tags/<tag>, :73-77).
- No tag ruleset: per candidate 20261017-vanity-deploy-environment-is-tag-gated, one branch ruleset `gt` on main (PR+squash, ci-gate, lydite/referral, no bypass); tags/protection 404. Live settings, not in git. ADR-0033:156 calls a tag ruleset "a repository setting a human applies". Nothing verifies tag signatures; no doc says tags are SSH-signed (grep ssh/signed/tag -s: 0 hits outside minisign ADR-0024/0026, which sign tool release assets, not git tags).
- verify-sibling-deps (bash, composite, no inputs) computes strict mode inline (:96-113) via one unauthenticated `git ls-remote --tags --refs` (:51) not git tags, so shallow checkouts are fine; "pending" = notes file with no tag, not exposed as an output. changed-projects (bash+jq, :124-143) builds edges from `go mod edit -json` requires, longest-prefix owner, closure of dependents; output is a JSON list, no topological order; reusable only as a pattern.
- ADR-0006:62-66 and release skill step 4: the release PR drops the dependent's replace and raises its require to the sibling tag BEFORE merge; strict mode then demands tagged siblings in the PR's CI. So chained releases need the dependency tagged before the dependent's PR can pass CI; release.yml's tag job (:171-181) also requires every sibling require to be already tagged. A bot never needs to edit go.mod after tagging if the PR is prepared by a human.

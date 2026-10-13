---
about: "ls-remote tag check is unchanged but now at action.yml:74; replace rejection :63-66 now says a sibling is reached through go.work"
saw:
  - .github/actions/verify-sibling-deps/action.yml
targets: verify-sibling-deps-tag-check-unauthenticated
verdict: still-true
---
Re-read the action: `git ls-remote --exit-code --tags https://github.com/${GITHUB_REPOSITORY}.git refs/tags/source/<sib>/<ver>` with `>/dev/null 2>&1` is at `:74` (note said `:55`); pseudo-version regex `:71`, error `:72` (note said `:53`); replace rejection `:63-66` (note said `:46`). Claim holds, anonymous, any failure = "no tag". The module-path check (`:57-61`) now compares against `go.vipengele.org/<dir>`.

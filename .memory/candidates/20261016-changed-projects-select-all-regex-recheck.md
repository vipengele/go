---
about: "the select-all regex still exists but moved to action.yml:93; per-project selection is now a longest-prefix jq owner() lookup, not grep -oE '^source/[^/]+'"
saw:
  - .github/actions/changed-projects/action.yml
targets: changed-projects-select-all-regex
verdict: still-true
---

Re-checked because the note was stale. `grep -n 'ci-\[a-z\]' action.yml` -> line 93, same regex
`^(\.github/(workflows/ci-[a-z]+\.yml|actions/)|\.lydite/)` (note said :63). The "otherwise" part
changed: selection is now `owner($path)` (action.yml ~L100-110), the project whose identifier is the
longest prefix at a `/` boundary, so nested projects (cli/core, cli/spec) select correctly; the
`grep -oE '^source/[^/]+'` (:69) form is gone. Dependents are added by a closure over go.mod
`require` edges of go.vipengele.org/<path>. The no-usable-base fallback (select all) is at ~L84-88.
Rest of the claim (go.work, release.yml, gt-*.yml select nothing) holds: they match neither branch.

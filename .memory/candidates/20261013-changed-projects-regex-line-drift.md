---
about: "select-all regex is now at changed-projects/action.yml:93 and still excludes go.work and release.yml; edges come only from go.mod Require (:114-122)"
saw:
  - .github/actions/changed-projects/action.yml
targets: changed-projects-select-all-regex
verdict: still-true
---
Regex `^(\.github/(workflows/ci-[a-z]+\.yml|actions/)|\.lydite/)` is at `:93` (note said `:63`); project selection by `owner()` longest prefix `:99-109` (note's `:69` grep is gone); no-base fallback `:83-88` (note `:54-58`). A change to `go.work` alone still selects nothing; relevant if workspace membership starts to matter to CI.

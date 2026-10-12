---
about: "Three CI files enforce that a project directory contains no other go.mod: changed-projects and verify-sibling-deps fail on any go.mod pair where one directory is a proper ancestor of the other, release.yml fails a tag whose project has a go.mod above or below it"
saw:
  - .github/actions/changed-projects/action.yml
  - .github/actions/verify-sibling-deps/action.yml
  - .github/workflows/release.yml
---

Go leaves a nested module out of the enclosing module's `./...`, while `changed-projects` gives a
changed file to the project with the longest matching directory, so two modules in one tree would be
selected and tested inconsistently. `changed-projects/action.yml:62-70` and
`verify-sibling-deps/action.yml:42-49` compare every pair of `go.mod` paths with
`[[ "$inner" != "$outer" && "$inner" == "${outer%/go.mod}/"* ]]` (the trailing `/` keeps `source/ab`
out of `source/a`) and fail with an `::error file=<inner>::`; `release.yml:72-88` checks the one
tagged project's ancestors up to `source/` and `find -mindepth 2` below it. The directory above a
group of projects (`source/cli`) holds no `go.mod`; adding one there fails all three. The check
reads every `go.mod` under `source/`, so a fixture module under a project's `testdata/` fails it too.

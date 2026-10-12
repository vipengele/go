---
about: "A workflow named ci-<letters>.yml, including ci-tools.yml for tools/, matches changed-projects' shared-CI regex, so changing it selects every Go project for build and test; deploy-*.yml would select none"
saw:
  - .github/actions/changed-projects/action.yml
  - .github/workflows/ci-tools.yml
  - .github/workflows/deploy-vanity-worker.yml
---

`.github/actions/changed-projects/action.yml:94` treats any changed file matching
`^\.github/workflows/ci-[a-z]+\.yml` as "a shared CI file changed; selecting every project".
`ci-tools.yml` (the npm checks for `tools/vanity-worker`) matches, so a pull request that edits it
builds and tests every project under `source/`, although the workflow itself checks no Go code.
A change under `tools/` alone selects no project (only files under a `source/` project directory
select one, `:108`), so build and test skip and `ci-tools.yml` is the only thing that checks it.

`deploy-vanity-worker.yml` does not match the pattern (prefix `deploy-`), so editing it selects
nothing. Name a workflow `ci-<letters>.yml` only when it should re-run every project.

---
about: "project detection runs independently in preflight, ci-build and ci-test, and the actions rely on the runner's preinstalled go and jq"
saw:
  - .github/actions/changed-projects/action.yml
  - .github/actions/verify-sibling-deps/action.yml
  - .github/workflows/ci-build.yml
  - .github/workflows/ci-test.yml
  - .github/workflows/ci-preflight.yml
---

Kind: invariant + gotcha.

1. `changed-projects` is invoked separately by ci-preflight.yml:374, ci-build.yml:49 and
   ci-test.yml:427, each with its own `fetch-depth: 0` checkout. The three must stay in
   agreement: preflight only decides skip-vs-run (empty vs not); each stage re-derives its own
   matrix. Changing selection logic in only one caller (e.g. an inline filter) makes the stages
   disagree. Each caller needs `fetch-depth: 0` or `git cat-file -e base` fails and the action
   silently falls back to selecting EVERY project (action.yml:54) - correct but slow, and it hides
   the misconfiguration.

2. Both composite actions call `go mod edit -json` and `jq`, but none of the `detect` jobs run
   `actions/setup-go`. They depend on `ubuntu-latest` shipping Go and jq. Moving `detect` to a
   runner image without Go makes the action fail (set -e) rather than select anything.

3. `verify-sibling-deps` runs ONLY in ci-build's detect job (ci-build.yml:47), not in ci-test or
   preflight. When preflight reports no affected project, build is skipped, so the sibling check
   does not run for docs-only changes either. Its only other caller is the release workflow
   (via `expect-tag`).

What breaks: weakened/duplicated selection, or a detect job on a bare runner failing every PR.
Evidence: reading the files above; grep for setup-go shows it only in the matrix jobs.

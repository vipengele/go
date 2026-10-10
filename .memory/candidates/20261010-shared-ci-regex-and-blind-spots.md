---
about: "changed-projects selects-all only for ci-[a-z]+.yml, actions/ and .lydite/; go.work, dependabot.yml, other workflows and root configs select no project"
saw:
  - .github/actions/changed-projects/action.yml
---

Kind: gotcha.

The "shared CI file changed, select every project" rule is one regex,
`action.yml:63`: `^(\.github/(workflows/ci-[a-z]+\.yml|actions/)|\.lydite/)`.
Anything else outside `source/<project>/` selects NO project (`:69` only greps `^source/[^/]+`).

What slips through (all select nothing, so build/test are skipped and the gate passes):
- `go.work`, `.gt-repo.yaml`, `.github/dependabot.yml`, `release.yml`, `gt-*.yml`.
- A stage workflow with a hyphen or digit in its name (`ci-build-extra.yml`, `ci-e2e2.yml`):
  `[a-z]+` does not match it, so edits to it do not re-run every project.

What breaks: a change that can alter every project's result is merged without CI exercising it.
Fix is to widen the regex when adding such a file, not to rely on the file being "CI".

Evidence: read of action.yml lines 63-69; no other path-based selection exists in the file.

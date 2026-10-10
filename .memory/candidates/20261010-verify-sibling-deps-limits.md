---
about: "verify-sibling-deps checks tags via unauthenticated ls-remote of the public github URL, only flags replaces whose Old path is a sibling, and does not read go.work"
saw:
  - .github/actions/verify-sibling-deps/action.yml
---

Kind: gotcha.

- Tag existence (`action.yml:151`) is `git ls-remote --exit-code --tags https://github.com/$GITHUB_REPOSITORY.git refs/tags/source/<sib>/<ver>` with no credentials. If the repo is private, or the
  proxy is unreachable, every sibling requirement reports "has no tag" - indistinguishable from a
  genuinely unreleased version because stderr is sent to /dev/null and any failure is treated as
  missing.
- A pseudo-version is detected by regex `v\d+.\d+.\d+-.*[0-9]{14}-[0-9a-f]{12}` (`:148`); it is
  rejected before the tag lookup. A release must be tagged BEFORE the PR requiring it can pass
  (ADR-0002): a "two projects in one PR" change cannot go green.
- The `replace` check (`:144`) matches only `Old.Path` starting with the sibling prefix. A replace
  of a non-sibling module, or go.work `use`/`replace`, is not inspected - go.work is deliberately
  outside the check because CI runs GOWORK=off.
- The module-path check only compares against `github.com/vipengele/go/source/<dir>`; a `/v2`
  major-version suffix (allowed by the module-path rule) would be reported as a mismatch.
- `expect-tag` only validates the tag form and that the project dir has a go.mod (`:159-165`);
  the sibling-tag loop runs for all projects regardless of it.

What breaks: false failures on a private repo / v2 module; false confidence that go.work or non-sibling replaces are covered.
Evidence: read of action.yml lines 126-167.

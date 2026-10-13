---
about: "the ls-remote tag check is now one cached listing at action.yml:59-72 whose failure stops the check, and a sibling replace is accepted in development mode: the note's 'any failure = no tag' and 'replace always rejected' no longer hold"
saw:
  - .github/actions/verify-sibling-deps/action.yml
targets: verify-sibling-deps-tag-check-unauthenticated
verdict: now-false
---
The listing is still anonymous (`git ls-remote --tags --refs`, `:62`), but a failure is an error that exits the action (`:62-65`), not "no tag"; the pseudo-version rejection is at `:160`; module path check `:103`. A sibling replace is rejected only if it names a version, resolves elsewhere than `source/<sibling>`, or the project is in strict mode. See 20261013-verify-sibling-deps-development-and-strict-modes for the full current rule.

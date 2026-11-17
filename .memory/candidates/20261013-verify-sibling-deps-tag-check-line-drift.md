---
about: "the ls-remote tag check is one cached listing in verify-sibling-deps/verify.sh whose failure stops the check, and a sibling directory replace is accepted in both modes (in strict mode at the sibling's pending or tagged version): the note's 'any failure = no tag' and 'replace always rejected' do not hold"
saw:
  - .github/actions/verify-sibling-deps/verify.sh
targets: verify-sibling-deps-tag-check-unauthenticated
verdict: now-false
---
The listing is anonymous (`git ls-remote --tags --refs`, `has_tag`, `verify.sh:26`), and a failure is an error that exits the check, not "no tag". The pseudo-version rejection is at `verify.sh:134`; module path check `:76`. A sibling replace is rejected if it names a version (`:111`) or resolves elsewhere than `source/<sibling>` (`:122`), or, in strict mode, if the sibling is required at a version neither pending nor tagged (`:138`). See 20261013-verify-sibling-deps-development-and-strict-modes for the full rule.

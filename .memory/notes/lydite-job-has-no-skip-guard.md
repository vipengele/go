---
name: lydite-job-has-no-skip-guard
kind: rationale
description: The lydite job in ci-orchestration.yml runs on !cancelled() with no skip guard and lydite-baseline is absent from ci-gate needs, both on purpose.
anchors:
  - path: .github/workflows/ci-orchestration.yml
    blob: f09c5de0d2ee
confidence: verified
---

`lydite` (`ci-orchestration.yml:148-159`) is the only publisher of the required `lydite/referral` status, which must exist for every SHA including merge_group commits; a skip guard like the other stages' `attest.validated != 'true'` would stall the merge queue on a check nothing can report. `lydite-baseline` (`:176-180`, push-only) is deliberately not in `ci-gate`'s `needs` (`:193`) so a failed baseline record does not turn main red. Do not "tidy" either into the guarded form. See also [[lydite-clearance-is-separate-workflow]].

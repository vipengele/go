---
name: lydite-clearance-is-separate-workflow
kind: rationale
description: gt-lydite-clearance.yml is its own issue_comment workflow because comment-triggered runs use the default branch's workflow files, so it cannot be tested from a PR.
anchors:
  - path: .github/workflows/gt-lydite-clearance.yml
    blob: c772b3a4fa3c
confidence: verified
---

`.github/workflows/gt-lydite-clearance.yml:4-10`: an `issue_comment` run always executes the default branch's copy of workflows, never the PR's, so changes to this file cannot be exercised from the PR that makes them. Folding it into the pull_request-triggered `ci-orchestration.yml` would run the whole pipeline on every comment. `/lydite clear` is what unblocks the `lydite/referral` status branch protection requires, see [[lydite-job-has-no-skip-guard]].

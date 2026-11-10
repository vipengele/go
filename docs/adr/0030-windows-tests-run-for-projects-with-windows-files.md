# Windows tests run for projects with Windows files

Status: accepted. It refines ADR-0019 (`0019-the-file-lock-is-its-own-project-filelock.md`), whose
Windows verification is a cross-compile, and the cost-based rejection of a Windows runner "for every
project" in ADR-0009 (`0009-platform-paths-live-in-cli-spec-paths.md`). It does not reverse either:
the runner is still refused for a project with no Windows-specific code.

A cross-compile with `GOOS=windows go vet` shows that a Windows file type-checks, not that it
behaves: a fault in a `LockFileEx` call or in the replacement of a running binary reaches a release
unseen. `ci-test.yml` therefore runs the suite on Windows for the projects that hold Windows code.

## Decisions

- **Selection by files.** The `test-windows` job takes the affected projects from `changed-projects`
  and keeps those holding a `*_windows.go` or `*_windows_test.go` file. It is skipped when that set
  is empty.
- **Command.** Each project in the matrix runs `GOWORK=off go test -race -shuffle=on ./...` on
  `windows-latest`, under bash, with a 30-minute timeout.
- **Gating.** `ci-gate` needs the `ci-test` reusable workflow, so a failing Windows job blocks the
  merge.
- **Cross-vet stays.** `ci-build.yml` and `release.yml` keep running `GOOS=windows go vet ./...`
  for a project with a `*_windows.go` or `*_windows_test.go` file. `release.yml` runs no Windows
  tests; the pull request that carries the release has already passed them.
- **Coverage stays on Linux.** The lydite stage measures the Linux suite only. Every Windows-only
  behaviour also has a Linux test with an injected fault, so the coverage figure does not depend on
  the Windows job.

## Considered options

- **A Windows matrix for every project.** It is the cost ADR-0019 and ADR-0009 rejected: most
  projects hold no Windows code, and a Windows runner is slower and dearer than a Linux one for the
  same result.
- **A job hard-wired to `update`.** It covers the one project that needs it today, and the next
  project with a `*_windows.go` file is unchecked until someone edits the workflow. Selecting by
  file needs no edit.
- **A nightly, non-gating run.** It costs nothing per pull request, and a Windows fault merges
  first and is found after, when the change that caused it is no longer the one in review.

## Consequences

- **A Windows fault blocks the pull request that introduces it.** The Windows path is tested, not
  only compiled.
- **A Windows-only project costs one runner per affected project.** A project with no Windows file
  adds none.
- **A new project with a Windows file is covered without a workflow change.**

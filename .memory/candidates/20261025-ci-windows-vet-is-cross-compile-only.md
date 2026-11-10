---
about: "Windows code is vetted by cross-compile in ci-build.yml and release.yml, and tested on the gated test-windows job in ci-test.yml for affected projects holding a *_windows.go or *_windows_test.go file; a release runs no Windows tests"
saw:
  - .github/workflows/ci-build.yml
  - .github/workflows/ci-test.yml
  - .github/workflows/release.yml
  - docs/adr/0030-windows-tests-run-for-projects-with-windows-files.md
---

- `ci-build.yml` and release.yml's `test` job run `GOOS=windows go vet ./...` when the project holds
  a `*_windows.go` or `*_windows_test.go` file (a `find` detect step sets an output).
- `ci-test.yml`'s `detect` job has a `windows` step that filters the affected projects to those with
  such a file into the `windows-projects` output; `test-windows` (windows-latest, bash,
  `CGO_ENABLED=1` for -race, 30-minute timeout) runs `go test -race -shuffle=on ./...` per project
  and is skipped by `if: ... != '[]'` when none qualifies. A skipped job leaves the reusable
  workflow a success, so `ci-gate` passes it; a failing one blocks the merge.
- A file named `*_windows_test.go` alone is enough to put a project on the Windows runner.

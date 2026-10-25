---
about: "CI vets Windows files by cross-compile only, in ci-build.yml; release.yml's test job does not cross-vet, and no CI step runs Windows"
saw:
  - .github/workflows/ci-build.yml
  - .github/workflows/release.yml
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - source/filelock/lock_windows.go
---

- `ci-build.yml` runs `GOOS=windows go vet ./...` after golangci-lint for any project whose
  directory holds a `*_windows.go` file (a detection step sets an output; the vet step is skipped
  otherwise). Only `source/filelock/lock_windows.go` matches today. Nothing executes Windows code.
- `release.yml`'s `test` job runs `go build` and `go vet` for the tagged project on Linux only, so a
  release does not repeat the Windows vet; it relies on `ci-build` having run on the merge.

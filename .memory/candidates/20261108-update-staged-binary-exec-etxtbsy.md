---
about: "Executing a staged binary in source/update: its write descriptor must be closed first (ETXTBSY), closeFile closes without settling, and tests that exec one cannot run in parallel"
saw:
  - source/update/extract.go
  - source/update/extract_unix_test.go
  - source/update/preflight.go
  - source/update/updatecmd/updatecmd_test.go
  - docs/adr/0029-apply-preflights-the-staged-binary-with-version-json-before-the-swap.md
---

- `Extract` returns the `*Staged` with its `os.CreateTemp` write descriptor still open, and Linux fails an `execve` of a file open for writing with `ETXTBSY` ("text file busy"). `Staged.closeFile` (`source/update/extract.go`) closes it without settling, so `Discard` still removes the file and `Keep` still works; `Keep` itself would settle and make a later `Discard` a no-op. `CheckStaged` (`source/update/preflight.go`) calls `closeFile` before running the binary.
- A process forked by any concurrently running test inherits every descriptor open at that instant until it execs. A parallel test can therefore hold another test's staged file open for writing and make its exec fail with `ETXTBSY` intermittently, even after `closeFile`. Every test that execs a staged file is non-parallel: `TestStagedRunsAfterCloseFile` (`source/update/extract_unix_test.go`), the real-exec tests in `preflight_unix_test.go`, and the real-install tests in `source/update/updatecmd/updatecmd_test.go`. Removing `closeFile` makes `TestStagedRunsAfterCloseFile` fail with "text file busy".
- Go runs top-level non-parallel tests before releasing paused parallel ones, and examples after all tests, so a real exec in a non-parallel test or an example does not overlap a parallel test.

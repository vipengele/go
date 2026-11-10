---
about: "updatetest.ReleaseExecutable is the single source of the fake release binary: the shell script ReleaseBinary off Windows, a Go program built at test time on Windows, so real install tests run on every OS"
saw:
  - source/update/updatetest/server.go
  - source/update/updatetest/program.go
  - source/update/apply.go
  - source/update/updatecmd/updatecmd_test.go
  - source/update/example_unix_test.go
  - source/update/apply_test.go
---

- `Config.Apply` always runs the staged binary with `version --json` (`CheckStaged`, `source/update/apply.go`), so a release used for a real install must answer it as the selected tag for `runtime.GOOS`/`GOARCH`. `updatetest.NewRelease` packs `updatetest.ReleaseExecutable(t, tag)` (`source/update/updatetest/server.go`): off Windows the `#!/bin/sh` script `ReleaseBinary(tag)`, with the tag and JSON single-quoted in; on Windows a Go program (`releaseProgramSource` in `updatetest/program.go`) with the tag and JSON templated in as `strconv.Quote` literals, printing the same bytes. `updatetest` cannot import `update`, so the JSON is built from its own struct (`versionDocument`).
- The Windows program is built per call with the go command on the PATH, in a throwaway module under `t.TempDir()` with `GOWORK=off`, `-trimpath`, and `GOOS`/`GOARCH` from the target. There is no in-process cache: `gochecknoglobals` and the update convention forbid package-level mutable state, and a relink with a warm go build cache takes about 0.1 s on Linux. The build is reproducible, so two calls for the same tag and platform return equal bytes.
- Tests that check installed bytes compare against `updatetest.ReleaseExecutable(t, tag)` (`apply_test.go`, `download_test.go`, `updatecmd_test.go`'s `releaseBinary`), never `ReleaseBinary` or a literal copy, so they hold on Windows too.
- The Windows branch is covered on Linux through the unexported `releaseExecutable(t, tag, goos, goarch)`: a cross-compiled windows/amd64 build is checked for the `MZ` header, and a host build is run and compared with the script's output (`updatetest/program_internal_test.go`).
- `Example_install` stays in `source/update/example_unix_test.go` under `//go:build unix`: an example function has no `testing.TB` to build the program with, so it packs `ReleaseBinary` directly. In-package `Apply` tests that run in parallel stub the exec seam `fsSteps.run`; the ones that execute the staged binary are not parallel (ETXTBSY).

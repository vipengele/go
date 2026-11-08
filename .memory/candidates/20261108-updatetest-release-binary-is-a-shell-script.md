---
about: "updatetest.ReleaseBinary is the single source of the fake release binary: a POSIX shell script answering version --json, so every real install test is unix-only"
saw:
  - source/update/updatetest/server.go
  - source/update/apply.go
  - source/update/updatecmd/updatecmd_test.go
  - source/update/example_unix_test.go
  - source/update/apply_test.go
---

- `Config.Apply` always runs the staged binary with `version --json` (`CheckStaged`, `source/update/apply.go`), so a release used for a real install must answer it as the selected tag for `runtime.GOOS`/`GOARCH`. `updatetest.ReleaseBinary(tag)` (`source/update/updatetest/server.go`) builds that answer as a `#!/bin/sh` script, and `NewRelease` packs it. The tag and JSON are single-quoted into the script. `updatetest` cannot import `update`, so the JSON is built from its own struct.
- Tests that check installed bytes compare against `updatetest.ReleaseBinary(tag)` (`apply_test.go`, `download_test.go`, `updatecmd_test.go`'s `releaseBinary`), never a literal copy of the script.
- `updatecmd` tests are in an external package and cannot stub the unexported exec seam `fsSteps.run`, so their real-install tests run the script and skip on Windows (`skipWithoutShell`). `Example_install` lives in `source/update/example_unix_test.go` under `//go:build unix` for the same reason. In-package `Apply` tests stub `run` instead.

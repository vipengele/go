---
about: "updatetest.NewRelease builds the archive the updater looks for on runtime.GOOS: zip with <binary>.exe on Windows, tar.gz with <binary> elsewhere, so the update suite passes on the Windows runner"
saw:
  - source/update/updatetest/server.go
  - source/update/updatetest/server_test.go
  - source/update/extract.go
---

- `NewRelease` uses `asset.DefaultExtFor(runtime.GOOS)` for both `BuildArchive` and `ArchiveName`,
  and names the entry `binaryFileName(binary, runtime.GOOS)` (".exe" on windows), mirroring
  extract.go's own `binaryFileName(Config)`.
- A test that pins `c.GOOS = "windows"` with an unset AssetExt now resolves zip; tests that need a
  tar.gz on any GOOS set `AssetExt` explicitly (extract_tar_test.go `tarGzConfig`).
- The entry body is still a `#!/bin/sh` script; tests only compare its bytes, so a test that ran
  the installed binary would fail on Windows.

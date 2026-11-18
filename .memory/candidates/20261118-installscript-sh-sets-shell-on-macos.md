---
about: "installscript tests run the script under the host's sh; on macOS /bin/sh is bash, which defines SHELL when the environment lacks it, so the unset-SHELL warning can only be tested under dash-like sh and the test skips elsewhere"
saw:
  - source/update/installscript/script_test.go
  - source/update/installscript/render_test.go
  - source/update/installscript/install.sh.in
---

- `shPath` in `render_test.go` resolves `sh` from `PATH`; `harness.Run` gives the script exactly `h.Env`. On macOS that sh is bash (posix mode), which sets `SHELL` from the passwd entry when it is absent, so `install_completion` in `install.sh.in` never reaches its "SHELL is not set" branch. On Linux CI `sh` is dash and leaves it unset.
- `TestScriptWarnsWhenShellIsNotSetAndSkipsTheCompletion` probes with `shSetsShell` (an empty-environment run of the same sh) and skips when it reports a value.

---
about: "the completion path table (Go) and the sh snippet are two implementations of one fact; script_test.go is the only thing that keeps them equal, and the snippet re-implements path.Clean"
saw:
  - source/cli/spec/completioninstall/table.go
  - source/cli/spec/completioninstall/script.go
  - source/cli/spec/completioninstall/script_test.go
  - source/cli/spec/completioninstall/install.go
  - source/update/hook.go
---
Verified 2026-10-26 by reading the code and mutating the snippet.
- table.go builds every path with path.Join, so inputs are cleaned as a whole (trailing and doubled slashes, "." and ".." in HOME, XDG_*, HOMEBREW_PREFIX). script.go's `_ci_clean` reproduces path.Clean in sh for that reason; changing a segment or a default in one file without the other is caught only by the cross-check in script_test.go, which runs the snippet under `sh` against `Target`.
- The zsh Homebrew branch decides usability twice: Go probes with a temp file in install.go (`dirWritable`), the snippet uses `[ -d ]` and `[ -w ]`. Both are true for root, so the unwritable-brew case in script_test.go skips when run as root and only runs under a non-root user.
- Install takes `spec.Identity` (its Name is the file name, its EnvVar names the opt-out variable); an invalid Identity is an error, but `Options.Disabled` returns before validation.
- update/hook.go never imports completioninstall; the hook receives the installed binary's path because the old process is still running after the swap, so `os.Executable()` names the wrong file (the wiring is in source/update/example_test.go).

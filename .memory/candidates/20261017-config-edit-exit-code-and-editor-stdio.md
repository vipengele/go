---
about: "config edit refuses a non-terminal stdin with exit 1 (not 2) because the usage-error type is internal/cobra's; its default editor launcher needs *os.File stdio so the editor reaches the terminal"
saw:
  - source/cli/core/command/config/edit.go
  - source/cli/core/exitcodes.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/internal/atomicfile/atomicfile.go
  - docs/adr/0017-the-config-command-group-is-an-opt-in-app-option.md
---

- `command/config` cannot return a usage error: `*cobra.UsageError` is the only route to exit 2 (`exitcodes.go`
  rejects `spec.Coded(ExitUsage, ...)` for registration), and depguard keeps cobra out of `command/config`. So
  `ErrNoTerminal` in `edit.go` exits 1.
- `launcher` in `edit.go` type-asserts `env.Stdin/Stdout/Stderr` to `*os.File` and returns `ErrNotFileStdio`
  otherwise: `exec.Cmd` copies any other reader or writer through a pipe and the editor then gets no terminal.
- `atomicfile.Replace` chmods after the editor exits, by opening the temp path at call time, because editors that save
  via rename leave a new inode at the umask default; the temp directory `.edit-*` is removed on every exit path.

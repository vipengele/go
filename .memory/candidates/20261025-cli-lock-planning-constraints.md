---
about: "cli state lock (issue #39): no Windows in CI, state project deliberately takes no lock and no non-YAML dependency, no consumer wired yet"
saw:
  - docs/adr/0018-the-state-store-is-its-own-project-cli-state.md
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
  - source/cli/state/doc.go
  - source/cli/state/AGENTS.md
  - .github/workflows/ci-test.yml
  - .github/workflows/ci-build.yml
  - source/cli/core/go.mod
---

Found while planning #39 (cross-process file lock).

- ADR-0018 says "No lock yet ... Serialising them is issue #39" and keeps cli/state's only dependency
  the YAML lib (state/AGENTS.md "Gotchas"); state/doc.go says the package "takes no lock".
  cli/state requires no sibling. cli/core requires cli/state (core/go.mod:9,28), used only in
  core/command/config/edit.go. Nothing consumes a lock today.
- CI is ubuntu-latest only: `grep -n runs-on .github/workflows/ci-{build,test}.yml` -> all ubuntu-latest, no
  GOOS matrix, no windows. ADR-0009 explicitly rejected a Windows/macOS matrix and build-tagged per-OS
  files in favour of run-time `Options.GOOS` injection. A LockFileEx implementation cannot use that
  trick, so its Windows code is compile/lint-checked only if the story adds it.
- Existing Windows handling in tests is `runtime.GOOS == "windows"` skips (state/write_test.go:42,
  load_test.go:205,409) and state/write.go uses `syscall.EINVAL/ENOTSUP` untagged.
- golang.org/x/sys is only an indirect dep (cli/spec/go.mod:11, cli/core/go.mod:18); no source file imports it.

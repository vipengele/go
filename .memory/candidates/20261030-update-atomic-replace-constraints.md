---
about: "constraints on a binary-replace step in source/update: ADR-0019 file-lock owns the Windows vet rule, no replace/rollback/typed-hint exists yet, fault-injection pattern is a steps struct, CI tests run as root"
saw:
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - docs/adr/0021-extraction-derives-the-format-from-config-and-stages-the-binary-in-the-callers-directory.md
  - source/update/.golangci.yml
  - source/update/AGENTS.md
  - source/cli/spec/completioninstall/install.go
  - source/cli/state/write.go
---

- Two ADRs are numbered 0019. The Windows rule is `0019-the-file-lock-is-its-own-project-filelock.md`
  (Decisions, "Windows verification"): `GOOS=windows go vet ./...` runs for any project with a
  `*_windows.go` file; it is a cross-compile only, behaviour untested. No ADR mentions rename-aside
  of a running .exe. `source/update` has no `*_windows.go` file yet, so adding one switches the vet on.
- No replace/backup/rollback code, no hint-carrying typed error in update, commons or cli/spec
  (grep `Rename|Hint` source/update, source/commons). `update.Staged` (extract.go) is mode 0700,
  synced, in a caller-supplied dir (ADR-0021); `Keep` leaves it for the rename.
- update/.golangci.yml: `default: standard` + depguard + gochecknoglobals only. No gosec/wrapcheck.
- Fault-injection precedent: a struct of fs funcs (`atomicSteps` in cli/spec/completioninstall/install.go:297,
  `steps` in cli/state/write.go:20). update's own convention is that OS access goes through Config fields (AGENTS.md Conventions; `Executable` seam exists).
- Tests run as root here (`id -u` = 0); precedent skips unwritable-dir cases on `os.Geteuid()==0`
  (completioninstall/script_test.go:84,115). update's `*_unix_test.go` use `//go:build !windows`.
- Coverage: .lydite/components.yml `update` runs `go-test -race ./...`; no numeric threshold in repo; gate is PR-vs-baseline via reusable-lydite (ci-orchestration.yml:147-185).

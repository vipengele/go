---
about: "What binds a daemon service-manager package: runtime driver selection vs ADR-0019 build tags, daemon is no longer stdlib-only, no coverage threshold, daemon never released"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - docs/adr/0023-a-daemons-single-instance-is-a-filelock-try-lock-with-a-holder-record.md
  - source/daemon/AGENTS.md
  - source/daemon/.golangci.yml
  - source/daemon/go.mod
  - source/cli/spec/paths/paths.go
  - .lydite/components.yml
  - CONTEXT.md
---

Checked 2026-11-04 while planning vipengele/go#76 (service-manager interface). Corrected the same
day against origin/main 41e2e24 (#163), which made the first version wrong in three places.

- Not stdlib-only. `source/daemon/go.mod` requires `go.vipengele.org/cli/spec` and
  `go.vipengele.org/filelock` with directory replaces (`../cli/spec`, `../filelock`, and
  `../commons`, indirect), plus `golang.org/x/sys v0.49.0` indirect. `source/daemon/AGENTS.md`
  Conventions: "The root `daemon` package is not stdlib-only" (the `daemon/service`
  package itself imports only the standard library). `.golangci.yml` depguard denies cobra and
  `cli/core` everywhere and, outside `daemoncmd/`, every `go.vipengele.org/` import but `daemon`,
  `daemon/service`, `cli/spec`, `cli/spec/paths` and `filelock`. A direct `x/sys/windows/svc` requirement would still
  be new for daemon (ADR-0003 expects it; ADR-0019 is the precedent for scoping x/sys to one project).
- ADR-0003 assigns service managers to `daemon`, service commands to `daemon/daemoncmd`; cobra is
  denied everywhere in daemon by depguard, so that needs its own depguard decision.
- ADR-0009 prefers a runtime-selected table via injected GOOS over per-OS build tags; ADR-0019
  says build tags are right only for syscalls. A `_windows.go` file would trigger CI
  `GOOS=windows go vet` (cross-compile only).
- ADR-0023 ("Windows is covered by injection"): daemon holds no `*_windows.go` and "gets no
  artificial one"; Windows behaviour is covered by injected file-system failures and a manual
  `GOOS=windows go vet ./...`. Verified: `ls source/daemon/*_windows.go` -> none. Only GOOS use in
  daemon is `runtime.GOOS` in `instance_test.go:228`; the injection seam is `fileOps` in
  `instance.go` (`acquireInstance(id, opts, ops)`), and production code takes GOOS from
  `paths.Options.GOOS`.
- Coverage: no numeric threshold in `.lydite/*.yml`; lydite compares to a recorded baseline.
- daemon is unreleased (`docs/release-notes` holds only commons/v0.1.0.md at the time), so no
  strict mode.
- Errors: sentinel `var ErrX = errors.New("daemon: ...")`, `%w` wrapping; gochecknoglobals allows
  sentinels only (`source/daemon/errors.go`).

---
about: "What constrains the daemoncmd service-command layer: spec.Command only, cli/spec only (cobra and cli/core denied by depguard), privileges checked up front through Manager.CheckPrivileges, no ADR for uninstall/data/elevation"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md
  - docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md
  - docs/adr/0013-stdout-carries-data-stderr-carries-messages.md
  - docs/adr/0014-json-output-envelope.md
  - docs/adr/0024-service-drivers-are-selected-at-runtime.md
  - docs/adr/0029-the-container-runtime-supervises-a-containerised-daemon.md
  - source/daemon/.golangci.yml
  - source/daemon/daemoncmd/status.go
  - source/daemon/service/manager.go
  - source/daemon/service/doc.go
  - source/daemon/service/state.go
  - source/daemon/AGENTS.md
  - source/daemon/daemoncmd/service.go
---

Checked 2026-11-16 against the service commands in daemoncmd/service.go.

- ADR-0003's "daemoncmd may hold cobra" is superseded by ADR-0005: commands are `*spec.Command`
  (cli/spec), cobra lives only in `cli/core/internal/cobra`. `source/daemon/.golangci.yml`
  depguard `no-cli` (files `**`) denies `github.com/spf13/cobra` and `go.vipengele.org/cli/core`
  everywhere in daemon; `root-imports-only-the-instance-lock-siblings` (files `!**/daemoncmd/**`)
  lets daemoncmd import any sibling but cobra/cli/core stay denied. `daemoncmd/status.go` (NewStatus)
  is the precedent: `*spec.Command`, writes `spec.WriteJSON` at schema 1, no own --json flag
  (reads `inv.Output.Format()`; ADR-0014), returns `spec.Reported` for a printed-not-ok result.
- `service.Manager` (manager.go) has Prepare/Install/Uninstall/Start/Stop/Restart/CheckPrivileges/
  Status. `CheckPrivileges` is each driver's own `requirePrivileges("manage")` (systemd euid,
  launchd euid, windows `scm.elevated()`), nil for a user scope; Status needs no privileges.
  `daemoncmd/service.go` calls `CheckPrivileges` right after the ManagerFunc in every command but
  status, so an unprivileged run fails before Prepare or any hook.
- `service.New` order (manager.go): identity, container refusal (*ContainerError, `.Instead` text,
  ErrContainer), cfg validate, options, GOOS switch. ADR-0029. So a command can show `.Instead`.
- Status states (state.go): unknown, not_installed, stopped, starting, running, failed; `resolve`
  turns starting with restarts>0 into failed. Status JSON tags are state/enabled/pid/restarts/detail.
- Install = enable + restart, never `enable --now` (AGENTS.md systemd section, doc.go "Install and
  updates"). Uninstall leaves data, account, log files (doc.go). No ADR for "uninstall leaves
  data", elevation messages, or --json for service status: all unspecified.
- `daemoncmd/service.go` holds the service commands (NewService and the flat constructors); no
  abandoned attempt found in docs/adr, CONTEXT.md or candidates. docs/release-notes holds only commons/v0.1.0.md, so daemon
  is not in strict mode.

---
about: "Nothing in this repo records tumika's ServeListener defer-order deadlock; constraints a daemon Serve layer inherits from ADR-0014/0010/0012"
saw:
  - source/daemon/supervisor.go
  - source/daemon/doc.go
  - source/daemon/go.mod
  - docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md
  - docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md
  - docs/adr/0012-signals-are-registered-by-execute-and-injected-through-env.md
---

`grep -rIi 'ServeListener|ErrRestartRequired|defer-order|tumika|deadlock' /home/user/go` (excluding .git) -> 0 hits. The tumika deadlock is not recorded anywhere here; it lives only in issue #69 / tumika.

ADR-0014 says "Lifecycle `Serve` and health ... are later stories". Supervisor.Run (supervisor.go) already: bounds shutdown by one grace from context.Background (newStopContext), reverse-order Stop, no signals (doc.go "# Signals"), PanicFail default, joins errors (failure first). ADR-0010: exit codes live in cli/spec (Coded/Reported); daemon may require cli/spec but not cli/core (depguard in source/daemon/.golangci.yml:16). source/daemon/go.mod has no require at all as of 2026-10-26; ADR-0010 is the only recorded exit-code mapping, and nothing says 0 for a restart sentinel.

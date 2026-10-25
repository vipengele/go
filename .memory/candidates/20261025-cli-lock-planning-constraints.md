---
about: "cli state lock (issue #39): cli/state deliberately takes no lock and no non-YAML dependency; the lock lives in the filelock project; nothing consumes it yet"
saw:
  - docs/adr/0018-the-state-store-is-its-own-project-cli-state.md
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - source/cli/state/doc.go
  - source/cli/state/AGENTS.md
  - source/filelock/go.mod
---

- ADR-0018 keeps cli/state's only dependency the YAML library (state/AGENTS.md "Gotchas"), and
  state/doc.go says the package "takes no lock". ADR-0019 puts the lock in `source/filelock`
  instead, so `golang.org/x/sys` reaches a consumer only through that project.
- `source/filelock/go.mod` requires `golang.org/x/sys` directly; no other project's go.mod does.
  Nothing requires filelock yet: no consumer (cli/state, config, update, daemon) is wired to it.

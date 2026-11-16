---
about: "ADR-0003 assigns the daemon's pending/confirm/rollback flow to the daemon project, but daemon's go.mod does not yet require update, and Install has no pre-rename hook"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md
  - source/daemon/go.mod
  - source/update/go.mod
  - source/update/install.go
  - source/update/apply.go
  - source/cli/state/write.go
---
Found while planning vipengele/go#83.

- ADR-0003 table row `daemon` owns "the daemon's own update flow (pending, confirm, rollback, scheduled runner)"; dependency direction daemon -> cli, update; update requires no cli project except cli/spec (and cli/state per its go.mod).
- source/daemon/go.mod requires only cli/spec and filelock; `grep go.vipengele.org/update source/daemon` -> 0 hits. The first daemon use of update adds `require go.vipengele.org/update v0.0.0` plus `replace => ../update` (ADR-0006).
- Install (source/update/install.go) does rename target->target.old then staged->target, no hook between; Apply (apply.go) returns *Installed with backup on disk and Installed is memory-only, so a crash orphans the backup. A pending record must be written by the caller before Install, or via a new hook in update.
- Atomic file write precedent: cli/state WriteFile/Replace (temp, chmod, fsync, rename, dir fsync; no dir sync on Windows). update's own StateStore interface (Load/Save with ctx) in update/state.go is the pluggable-store precedent.
- No release notes exist for update or daemon (only docs/release-notes/commons/v0.1.0.md), so both are in development mode; tags not checked (no git metadata in the checkout).

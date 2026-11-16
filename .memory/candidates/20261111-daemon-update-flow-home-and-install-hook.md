---
about: "ADR-0003 assigns the daemon's pending/confirm/rollback flow to the daemon project; Install has a backup-guard check before its rename but no hook that writes a record"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md
  - source/daemon/go.mod
  - source/update/install.go
  - source/update/apply.go
  - source/cli/state/write.go
---
- ADR-0003 table row `daemon` owns "the daemon's own update flow (pending, confirm, rollback,
  scheduled runner)"; dependency direction daemon -> cli, update. source/daemon/go.mod requires
  `go.vipengele.org/update v0.0.0` with `replace => ../update` (ADR-0006).
- Install (source/update/install.go) asks `Config.BackupGuard` and then renames target->target.old
  and staged->target, with nothing that lets a caller write a record between them. Apply returns
  `*Installed` with the backup on disk and Installed is memory-only, so a crash after the rename and
  before the caller's record orphans the backup.
- Atomic file write precedent: cli/state WriteFile/Replace (temp, chmod, fsync, rename, dir fsync;
  no dir sync on Windows). update's own StateStore interface (Load/Save with ctx) in update/state.go
  is the pluggable-store precedent.

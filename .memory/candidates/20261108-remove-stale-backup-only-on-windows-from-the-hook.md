---
about: "UpdateCheckHooks removes the stale <exe>.old only when the host is Windows: on Linux and macOS a run started between Install and Rollback (a PostUpdate hook running the new binary) would delete the backup Rollback needs"
saw:
  - source/update/stale.go
  - source/update/checkhooks.go
  - source/update/apply.go
  - docs/adr/0031-update-on-windows.md
---

- `Config.RemoveStaleBackup` (stale.go) removes `<Target>.old` on any OS. The pre-run hook in
  checkhooks.go calls `c.removeStaleBackupOnHost(runtime.GOOS)`, which does nothing off Windows.
- Why: Apply returns an unsettled `*Installed`; the caller runs PostUpdate hooks before
  Discard/Rollback (apply.go doc). Apply holds the update lock only around its state record, not
  until settling, so a lock cannot guard the window. On Windows the backup of an unsettled update is
  the updating process's running image and cannot be removed (it reports `*BackupLockedError`), so
  the hook is safe there; off Windows Discard always removes the backup and only a crash leaves one,
  which the next Install replaces.
- The hook runs it before the `SkipUpdateCheck` early return, so every command cleans up.
  `Config.Start` does no executable I/O. The host check reads `runtime.GOOS`, an exception to the
  package's "GOOS through Config" convention recorded in source/update/AGENTS.md.

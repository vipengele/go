---
about: "update.Config.BackupGuard is consulted in removeStaleBackup after resolve(), and nothing yet wires the daemon's Machine into it"
saw:
  - source/update/stale.go
  - source/update/config.go
  - source/update/checkhooks.go
  - source/daemon/go.mod
---
- `removeStaleBackup` (stale.go) is the single removal site; the check hook reaches it through
  `removeStaleBackupOnHost(ctx, goos)` (checkhooks.go), which acts only on Windows.
- The guard runs after `c.resolve()`, so an invalid Config returns its Validate error before the
  guard is asked. In use keeps the backup with a nil result; a guard error keeps it and is returned
  wrapped. A nil `Config.BackupGuard` removes unconditionally.
- `*selfupdate.Machine` satisfies `update.BackupGuard` structurally (`BackupInUse(ctx)`), but
  source/daemon/go.mod does not require `go.vipengele.org/update`, so no code passes it yet; the
  guard does not protect a daemon's backup until a caller sets it.

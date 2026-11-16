---
about: "update.Config.BackupGuard is consulted by removeStaleBackup, by Apply before any request and by Install immediately before its first rename"
saw:
  - source/update/stale.go
  - source/update/config.go
  - source/update/checkhooks.go
  - source/update/apply.go
  - source/update/install.go
  - source/daemon/selfupdate/apply.go
  - source/daemon/daemoncmd/update.go
---
- `removeStaleBackup` (stale.go) is the single removal site; the check hook reaches it through
  `removeStaleBackupOnHost(ctx, goos)` (checkhooks.go), which acts only on Windows. The guard runs
  after `c.resolve()`; in use keeps the backup with a nil result, a guard error keeps it and is
  returned wrapped. A nil guard removes unconditionally.
- `Config.Apply` (apply.go) asks the guard after CheckDowngrade and before Download, so a refusal
  makes no request; `checkBackupGuard` (install.go) returns `ErrBackupInUse` wrapped for in use and
  the guard's error wrapped for a failure (fail closed). Install asks again right before renaming
  the target to its backup; the public `Install(staged)` takes no ctx and asks with
  `context.Background()`, while Apply passes its own ctx through `installWithin`.
- The daemon wires `*selfupdate.Machine` in as the guard: `Machine.Apply` and `daemoncmd.NewUpdate`
  each set it on their own copy of the Config. The Windows stale-backup hook is protected only when
  the consumer passes that same guarded Config to `UpdateCheckHooks`; nothing enforces it.

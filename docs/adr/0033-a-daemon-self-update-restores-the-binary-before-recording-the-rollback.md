# A daemon self-update restores the binary before recording the rollback

Status: accepted. It places the daemon's own update flow, which ADR-0003
(`0003-the-library-is-three-projects-cli-update-and-daemon.md`) assigns to `daemon`, in
`go.vipengele.org/daemon/selfupdate`. It rolls back with the backup that ADR-0022
(`0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md`) keeps, and on
Windows works around the running-executable rules that ADR-0031 (`0031-update-on-windows.md`)
describes.

A daemon replaces its own binary and the service manager restarts it into the new build. If the new
build cannot start, nobody is there to undo the update. `selfupdate` records the update in a
lockable store as a pending update, counts each boot of the updated build before it is judged, and
rolls the binary back when the build does not confirm itself. A record moves `idle` to `pending`,
then to `confirmed` or `rolled_back`.

## Decisions

- **A rollback restores the binary first and records `rolled_back` second.** The backup is put back
  (one rename on Unix; on Windows the target is moved aside to `<target>.failed` and the backup is
  renamed in, undone when the second step fails), and only then is the record written. A failure
  between the two leaves a `pending` record on a binary that boots, and the next boot finds a
  pending record whose `ToVersion` differs from the running version and records `rolled_back`
  without touching any file. A failure to restore leaves `pending`, so the rollback is tried again.
- **A corrupt record is an error, never idle.** `State`, `Begin`, `Boot`, `Confirm`, `MarkFailed`
  and `BackupInUse` return an error for a record that does not parse. The operator clears it by
  deleting the file. The store is the only memory of
  a pending update, so a corrupt pending record read as idle would turn rollback off while a
  possibly broken build runs.
- **`MarkFailed` rolls back at once.** A build that knows it cannot serve, such as a failed
  migration or an unreadable configuration, says so and the rollback follows without waiting for
  further boots. Without the call, such a build is rolled back only after it has been started more
  than `WithMaxBootAttempts` times, three by default.
- **The backup guard is an interface that `update` declares.** `update.Config.BackupGuard` has one
  method, `BackupInUse(ctx) (bool, error)`, which `RemoveStaleBackup` consults before it removes
  `<Target>.old`. `*selfupdate.Machine` implements it: `BackupInUse` is true only while an update is
  pending, and an unreadable record is an error, on which `RemoveStaleBackup` keeps the backup.
  `update` names no type of `daemon` and never imports it, which keeps the dependency direction of
  ADR-0003: `daemon` requires `update`, and no project requires `daemon`.

## Considered options

- **Recording first, then restoring.** The prior art in tumika's `rollBack` wrote `rolled_back` and
  then renamed the backup. A failure between the two left a record that says the update was rolled
  back on a binary that cannot boot, and nothing in the record shows it. The restore-first order
  leaves only a state the next boot detects and repairs.
- **Treating a corrupt record as idle.** `update` fails open when its own state is lost or corrupt,
  because that costs one extra check
  (`0023-update-keeps-its-check-state-through-cli-state.md`). Here the same choice would silently
  disable the rollback the record exists to drive, so the record fails closed.
- **Counting `MarkFailed` as one boot attempt.** A build that reports its own failure would run
  through the remaining attempts, each one a start the service manager performs, before it is
  rolled back, and the report would carry no more weight than a crash.
- **`update` importing `daemon/selfupdate`.** It makes every tool that wants only self-update
  inherit `daemon`, and ADR-0003 allows no project to require `daemon`.
- **`daemon` owning the removal of stale backups.** The `.old` file and its removal on Windows
  belong to `update` (ADR-0031), so a second remover would have to know when `update` may delete it
  and when `Rollback` needs it.

## Consequences

- **A consumer wires the guard once.** A daemon that self-updates sets its `Machine` as
  `Config.BackupGuard`. Otherwise the check hook on Windows, which calls `RemoveStaleBackup`, can
  remove the backup of an update awaiting confirmation.
- **The next boot repairs a rollback that stopped half way.** It costs no extra state: the
  difference between `ToVersion` and the running version is the evidence.
- **A damaged record needs an operator.** The daemon refuses to judge an update from a record it
  cannot read: `Boot`, `Confirm`, `MarkFailed` and `BackupInUse` return an error until an operator
  deletes the file. What a daemon does with that error is the caller's.
- **A failed restore is retried by each boot.** The record stays `pending`, so the boot count
  continues to exceed its limit and the rollback is attempted again.

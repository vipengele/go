---
about: "update.Config.Apply has no seam between preflight and the rename where Machine.Begin can record pending; the only pre-swap hook is Config.Preflight, and composing the steps by hand loses the HighestApplied watermark"
saw:
  - source/update/apply.go
  - source/update/install.go
  - source/update/preflight.go
  - source/update/config.go
  - source/daemon/selfupdate/machine.go
  - docs/adr/0029-apply-preflights-the-staged-binary-with-version-json-before-the-swap.md
  - docs/adr/0033-a-daemon-self-update-restores-the-binary-before-recording-the-rollback.md
---

Read 2026-11-16 while planning source/daemon/selfupdate's scheduled runner.

- `Config.Apply` (apply.go:72) runs steps 1-9 with no callback between `checkStaged` (apply.go:119) and `install` (apply.go:126). No ApplyOption exists besides AllowDowngrade and WithDownloadOptions (apply.go:19-29).
- The only code that runs after staging and before the renames is `Config.Preflight` hooks (preflight.go:~105 loop; config.go:137). ADR-0029 requires them side-effect-free, because a later hook or Install may still refuse. Begin inside a hook would leave a `pending` record for a swap that never happened (Boot later repairs it as rolled_back without touching files, machine.go Boot `ToVersion != running` branch).
- Neither ADR-0033 nor selfupdate/doc.go says "Begin before the swap". doc.go says pending is "once the new binary is installed and its backup kept". Only the Pending doc comment ("about to be replaced") implies pre-swap. Nothing in source/ calls Machine.Begin outside tests (grep `\.Begin(`).
- Target and backup are knowable up front: `Config.Target()` (target.go:54) and `backupSuffix = ".old"` (install.go:12), so backup is `Target()+".old"`. `Installed.Backup()` is Path+".old" (install.go:189) and is the same value.
- Composing by hand (CheckManaged, CheckWritable, CheckDowngrade, Download, Target, Extract, CheckStaged, Begin, Install) is possible because each step is exported, but `recordApplied` (apply.go:135) is unexported, so the HighestApplied watermark is NOT raised. Downgrade protection then falls back to the running Version only.
- If Begin comes after Install (using Installed.Path/Backup) the window is: binary swapped, no pending record; a crash there is unguarded. If Begin fails with ErrUpdatePending after Install, the caller must Installed.Rollback.

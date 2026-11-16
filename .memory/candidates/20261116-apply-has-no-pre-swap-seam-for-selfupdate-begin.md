---
about: "update.Config.Apply runs BeforeInstall hooks between preflight and the renames; that is where a daemon self-update records pending with Machine.Begin, because Preflight hooks must be side-effect free and a hand-composed Apply never raises HighestApplied"
saw:
  - source/update/apply.go
  - source/update/install.go
  - source/daemon/selfupdate/cycle.go
  - source/daemon/selfupdate/machine.go
  - docs/adr/0029-apply-preflights-the-staged-binary-with-version-json-before-the-swap.md
  - docs/adr/0035-apply-runs-a-before-install-hook-so-a-self-update-records-pending-before-the-swap.md
---

Checked 2026-11-16 after the scheduled self-update runner landed.

- `BeforeInstall(func(ctx, InstallPlan) error)` (apply.go:60) is an ApplyOption; the hooks run in
  `Config.apply` after `checkStaged` (apply.go:152) and before `install` (apply.go:163), loop at
  apply.go:157. Several calls add up, run in order, and the first error stops the rest.
- `InstallPlan{Target, Backup, Version}` (apply.go:23): Backup is Target + `.old`, the same path
  `Installed.Backup()` returns afterwards (install.go backupSuffix); Version is the Selection's Tag.
- A hook error discards the staged binary, renames nothing, skips `recordApplied` and is returned
  wrapped `update: before install: %w`, so errors.Is reaches the hook's error.
- Do not record pending from a `Config.Preflight` hook instead: ADR-0029 requires those to be
  side-effect free, since a later hook or Install can still refuse. Composing Apply's exported steps
  by hand is also wrong: `recordApplied` is unexported, so HighestApplied never rises (ADR-0035).
- The self-update cycle uses exactly this seam: cycle.go `applyWithHook` passes a hook that calls
  `Machine.Begin` (machine.go:92) with from/to/target/backup.

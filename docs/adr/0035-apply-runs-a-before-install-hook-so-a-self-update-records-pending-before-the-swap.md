# Apply runs a before-install hook so a self-update records pending before the swap

Status: accepted. It adds a step to the `Config.Apply` of "Apply preflights the staged binary with
`version --json` before the swap"
(`0029-apply-preflights-the-staged-binary-with-version-json-before-the-swap.md`), between that
preflight and the two renames of "Install replaces the executable with two renames and keeps a
backup" (`0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md`). It serves
the record of "A daemon self-update restores the binary before recording the rollback"
(`0033-a-daemon-self-update-restores-the-binary-before-recording-the-rollback.md`).

A daemon that updates itself records the update as `pending` with `selfupdate.Machine.Begin`, and
that record is what lets the next boot judge the new build and roll it back. The record has to
exist before the executable is swapped: a process that stops after the swap and before the record
leaves a new binary with nothing that would roll it back. `Apply` ran the preflight and then the
install with no point between them where a caller could act, and the steps it records after the
install, `recordApplied` and its `HighestApplied` watermark, are unexported.

## Decisions

- **An `ApplyOption`.** `BeforeInstall(func(ctx context.Context, plan InstallPlan) error)`. Several
  calls add up and the hooks run in the order given. The hook is per call, like
  `WithDownloadOptions`: a self-update records pending for the one `Apply` that swaps the binary,
  and an `update` command of the same tool, sharing its `Config`, must not.
- **`InstallPlan{Target, Backup, Version}`.** `Target` is the executable's resolved path, the one
  `Apply`'s `Target` step returns; `Backup` is `Target` followed by `.old`, the path
  `Installed.Backup` returns afterwards; `Version` is the `Selection`'s `Tag`. These are the fields
  `selfupdate.Pending` needs besides the running version, which the caller already has.
- **After the preflight, before any rename.** The hook runs once `CheckStaged` has passed, built-in
  check and `Preflight` hooks included, and before `Install` chmods or renames anything. Only
  `Install`, and any later `BeforeInstall` hook, can still refuse after it.
- **A hook may have side effects.** That is its purpose, and it is the difference from a
  `Preflight` hook, which stays side-effect-free because a later hook or the install may still
  refuse. A hook with side effects handles an `Apply` that fails after it: the executable is then
  as it was, or a `*RestoreError` names the backup holding the previous binary.
- **A hook error stops the install.** `Apply` discards the staged file, as it does on a refused
  preflight, renames nothing, skips the `HighestApplied` record and returns the error wrapped with
  `update: before install:`, so `errors.Is` matches it. An error removing the staged file is joined
  on.

## Considered options

- **`Begin` in a `Preflight` hook.** It runs at the right time, but ADR-0029 requires a preflight
  hook to be free of side effects: a later hook can still veto, and the record would then say
  `pending` for a swap that never happened. The next boot would find a `ToVersion` that is not the
  running version and record `rolled_back` for an update that was never applied. The hooks are also
  `Config` fields, shared by every `Apply` and `CheckStaged` the tool makes.
- **`Begin` after `Apply`.** The `Installed` names the target and the backup, but the binary is
  already swapped when `Begin` runs. A process that stops in between leaves the new build with no
  pending record, so nothing rolls it back if it cannot start, and a `Begin` that fails, with
  `ErrUpdatePending` say, leaves the caller to roll back an install it has just made.
- **Composing `Apply`'s exported steps by hand.** Every step but the record is exported, so a caller
  can call `Begin` between `CheckStaged` and `Install`. The record, `recordApplied`, is unexported:
  `HighestApplied` would never rise, and the downgrade guard would fall back to the running version
  alone. Exporting the record would make every such caller repeat `Apply`'s order and its cleanup.

## Consequences

- **A self-update records `pending` before the swap.** A process that stops after the hook and
  before the renames leaves a pending record on the old binary, which the next boot finds with a
  `ToVersion` other than the running version and repairs, as ADR-0033 describes, without touching
  any file.
- **The watermark rises as for any `Apply`.** A successful install with a hook raises
  `HighestApplied` to the installed tag.

# `update` on Windows

Status: accepted. It extends
[0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md](0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md),
whose backup a running Windows executable leaves behind, and
[0023-the-install-script-is-rendered-by-update-installscript-around-the-completion-snippet.md](0023-the-install-script-is-rendered-by-update-installscript-around-the-completion-snippet.md),
whose sh script is Unix only and refuses a zip asset. Its tests run under
[0030-windows-tests-run-for-projects-with-windows-files.md](0030-windows-tests-run-for-projects-with-windows-files.md).

A tool built on `go.vipengele.org/update` runs on Windows as on Linux and macOS: it checks for a
release, downloads and verifies it, and replaces its own executable. Three things differ there.
Windows lets a running executable be renamed but not removed, so the `<exe>.old` backup that
`Config.Install` keeps (ADR-0022) outlives the process that ran the update. The stock tools of a
Windows machine open a zip archive and no tar.gz. And a user installs the tool from PowerShell, not
from `sh`. `Install` keeps the one code path ADR-0022 gives it; `install_windows_test.go` replaces
a running executable on the gated `windows-latest` job, and the project's `*_windows_test.go`
files turn on its `GOOS=windows go vet` step (ADR-0030).

## Decisions

- **A stale backup is removed by `Config.RemoveStaleBackup` and by the check hook on Windows.**
  `RemoveStaleBackup` removes `<Target>.old` and returns nil when there is none; a backup a process
  still holds open is a `*BackupLockedError` and stays on disk. The pre-run hook of
  `Config.UpdateCheckHooks` calls it, best-effort with its error dropped, on every command, those
  carrying `spec.SkipUpdateCheck` included, and only when the host (`runtime.GOOS`) is Windows. The
  backup of an update not yet settled by `Installed.Discard` or `Installed.Rollback` is the
  updating process's running image there, so the hook cannot take it from Rollback. A tool that
  wires no hooks calls the method itself. `Config.Start` does no executable I/O.
- **zip is the default asset extension on Windows.** `asset.DefaultExtFor(goos)` is the one source
  of the default: `"zip"` for `"windows"` and `"tar.gz"` for every other GOOS. `Config.resolve`
  fills an empty `AssetExt` with it for the resolved GOOS, and `Validate` and the extractor derive
  the format from the same function. A non-empty `AssetExt` is used on every GOOS. `update` has no
  release, so the default binds no consumer to the other extension.
- **The PowerShell installer is in scope.** `installscript.RenderPowerShell(cfg)` renders a
  PowerShell script with the sh script's releases, checksum and signature policy, environment
  variables and order. It runs on Windows PowerShell 5.1 and PowerShell 7, saved to a file or as
  `irm <url> | iex`, and handles zip, tar.gz, tgz and raw-binary assets.
- **The PowerShell installer edits the user PATH.** When the install directory is on neither the
  session's PATH nor the user PATH, the script appends it to the raw value of
  `HKCU\Environment\Path`, read with `DoNotExpandEnvironmentNames` and written back with its own
  kind (`REG_EXPAND_SZ`, or `REG_SZ` when it was one; a missing value is created as
  `REG_EXPAND_SZ`), broadcasts `WM_SETTINGCHANGE` and adds the directory to the session's PATH.
  `<PREFIX>_NO_MODIFY_PATH=1` replaces all of that with a warning. The sh script only warns, and the
  difference is deliberate.
- **PowerShell completion comes from `cli/spec`.** `completioninstall.PowerShellSnippet(name)`
  renders the fragment and `RenderPowerShell` embeds it verbatim, as `Render` embeds `Snippet`
  (ADR-0023). The fragment writes the completion script to the file `completioninstall.Target`
  chooses for PowerShell and prints the line to add to `$PROFILE`; it never edits `$PROFILE`. It
  carries no opt-out guard: the embedding script skips it when `<EnvPrefix>_NO_COMPLETION` is `1`.
- **Each renderer resolves for its target.** `Render` resolves a copy of the Config with GOOS
  `"linux"` when it names none, and `RenderPowerShell` with `"windows"`. The sh script detects the
  system when it runs, so the GOOS decides only its defaults; a Config naming `"windows"` resolves
  `AssetExt` to `"zip"` and `Render` refuses it with `ErrZipAsset`.
- **An antivirus or indexer lock gets no retry loop.** A lock that fails the first rename leaves
  the executable untouched; one that fails the second rename restores the backup, and the user
  runs the update again. A lock that keeps `<exe>.old` from being removed leaves it for the next
  start of the tool to remove.

## Considered options

- **A sweep inside `Start`.** `Start` is documented to do no executable I/O, and a command
  carrying `spec.SkipUpdateCheck` never calls it, so the update command itself would never clean.
- **The method alone.** A tool built on `cli/core` that wires `UpdateCheckHooks` would get no
  automatic cleanup, and every tool would have to learn when calling it is safe.
- **Removing the stale backup on every OS.** Off Windows an open file can be removed, so a run
  started between `Install` and `Rollback`, a `PostUpdate` hook running the new binary or the user
  starting the tool, would remove the backup Rollback needs. Off Windows `Discard` always removes
  the backup, a stale one is left only by a crash, and the next `Install` replaces it.
- **tar.gz everywhere.** `Expand-Archive` and Windows' stock tools open a zip; a tar.gz needs the
  `tar.exe` of Windows 10 and later. A tool releasing zip for Windows would have to set `AssetExt`
  per platform, which `Config` carries no rule for.
- **Resolving with the host GOOS.** The sh script would depend on the machine that renders it: a
  Windows host would resolve `AssetExt` to `"zip"` and render no sh script at all.
- **Deferring the PowerShell installer, or the sh script on Windows.** The sh script refuses a zip
  asset, which is the Windows default, and a Windows machine has no `sh` to run it with.
- **Warning about PATH, as the sh script does.** A Unix user edits a shell rc file; a Windows user
  has no such convention, and a tool installed off PATH cannot be run by name.
- **`[Environment]::SetEnvironmentVariable('Path', ..., 'User')`.** It reads the expanded value and
  writes `REG_SZ`, so every `%VARIABLE%` reference in the user PATH is flattened to its current
  value.
- **Editing `$PROFILE`.** It is the user's file, its path differs between Windows PowerShell and
  PowerShell 7, and an edit that breaks it shows an error in every session the user opens.
- **The completion logic inline in `installscript`.** It would be a second copy of the completion
  path table, the drift ADR-0021 and ADR-0023 rejected.
- **A retry loop around the renames.** It delays every failing update by its budget, and a lock
  that outlasts it fails the same way; the restored backup already leaves the tool working.

## Consequences

- **A Windows update leaves `<exe>.old` until the next run.** `Discard` returns a
  `*BackupLockedError`, which the caller may ignore, and the next command of a tool wiring
  `UpdateCheckHooks` removes the backup.
- **A tool releases zip on Windows unless it sets `AssetExt`.** Its GoReleaser archive `format`
  for Windows must produce the same extension, or `Select` looks for a name the release lacks.
- **A tool has two install scripts.** `Render` serves Linux and macOS, `RenderPowerShell` serves
  Windows, and both read the same resolved defaults.
- **Windows PowerShell 5.1 is the minimum.** A tar.gz asset needs the `tar.exe` of Windows 10 and
  later.
- **`update` runs its suite on Windows.** Every pull request that selects it pays for a
  `windows-latest` runner (ADR-0030).

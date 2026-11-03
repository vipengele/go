# Install replaces the executable with two renames and keeps a backup

Status: extends ADR-0021, whose `*Staged` binary the install step consumes.

`Config.Install` takes the `*Staged` that `Config.Extract` returns and replaces the running tool's
executable with it. The previous binary is kept as a backup until the caller discards it, and is put
back if the replacement fails.

## The rules

- **Order.** Resolve the executable with `filepath.EvalSymlinks` and `filepath.Abs`; read its mode;
  require the staged file to be in the executable's directory, so both renames stay on one
  filesystem; `Keep` the staged file; chmod it by path to the executable's permission bits (setuid,
  setgid and sticky are dropped), so a 0700 file is never exposed at the real path; rename the
  executable to its backup; rename the staged file into place. Everything before the first rename
  fails without touching the executable.
- **Two renames, never one.** On Windows a running `.exe` can be renamed but not overwritten or
  deleted. The old binary moves aside first, then the new one moves in. A single rename over the
  executable, or a copy onto it, fails there.
- **The backup is `<target>.old`**, in the target's directory. A stale one is replaced by the
  rename. `Installed` lives in memory only, so a backup orphaned by a crash is invisible to the API
  and the next `Install` overwrites it.
- **`Install` owns the `*Staged`**, as `Extract` owns the archive. On any failure the staged file is
  removed by path. When the rename into place fails the backup is renamed back; when that fails too
  the error is a `*RestoreError` carrying the backup path, so the binary can be recovered by hand.
- **`Discard` and `Rollback` settle once.** The first call of either decides and later calls return
  nil. `Rollback` renames the backup over the target, which removes the new binary.
- **A locked backup is `*BackupLockedError`.** `Discard` returns it only when the resolved
  `Config.GOOS` is `windows` and the remove error unwraps to errno 5 or 32. Access denied matches
  too, so an ACL denial on Windows reads as locked. The update has succeeded and the caller may
  ignore it.
- **A permission failure is a `*PermissionError`** with a user-facing `Hint`, unwrapping to
  `fs.ErrPermission`. `CheckWritable` creates and removes a temp file named from `TempPrefix` in the
  target's directory so a caller can fail before downloading. It is best-effort: the directory may
  change afterwards, and `Install` is the authority.
- **Hard links are not updated.** Only the resolved path is replaced; another name for the same
  inode keeps the old binary. A symlink to the executable keeps working.
- **The seams are unexported.** `fsSteps` is set by in-package tests; `Config` gains no field. CI
  runs as root, so injection is how the permission path is tested.
- **`Install` runs no hook.** The caller runs `RunPostUpdate` with `Installed.Path()`.
- **Windows is one code path.** No `_windows.go` file, so the `GOOS=windows go vet` step stays off
  for this project, and Windows behaviour is unverified, as ADR-0019 (the file lock) accepts for
  `filelock`.

## Considered options

- **One rename, or a copy, onto the executable.** It fails on Windows, where a running executable
  cannot be overwritten or deleted, and a copy leaves a half-written binary at the real path if it
  is interrupted.
- **`github.com/minio/selfupdate`.** It does this job, but its dependency would enter every
  consumer's graph, and its backup and rollback shape is not the one the caller settles here.
- **Stat-based writability.** Mode bits are wrong for root, ACLs, read-only mounts and Windows. Only
  attempting the write answers.
- **A hint type in `commons`.** Nothing else needs it, and it would widen the sibling surface and
  force a `commons` release. `PermissionError` carries its `Hint` itself.
- **`_windows.go` files.** They would turn on the `GOOS=windows go vet` gate for code CI never
  runs.
- **A `Config` field for the filesystem seams.** It would make internals public API for the sake
  of tests; an in-package struct covers them.

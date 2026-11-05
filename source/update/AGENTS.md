# update

`go.vipengele.org/update`: checks for and applies updates to a command-line tool. Read `doc.go` first,
then `docs/adr/0008-update-takes-its-identity-from-cli-spec.md` at the repo root.

## Commands (run from `source/update`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Structure

- `config.go` — `Config`: embeds `spec.Identity` (from `go.vipengele.org/cli/spec`) and carries
  every seam to the outside world (HTTP client, clock, GOOS/GOARCH, executable, env lookup, user
  home dir, `EvalSymlinks`, `InContainer`, `IsTerminal`, `CheckWait`, `Locker`) and the tool's own
  `Detectors`; a nil `Detectors` entry is invalid. `Validate` reports problems; `resolve` returns a
  copy with each zero field set to its production default and never mutates the receiver. `Resolved`
  is its exported wrapper, the one place other packages read the defaults from (ADR-0023). The
  default state dir comes from `paths.Resolve(..., paths.State)` in
  `go.vipengele.org/cli/spec/paths`, resolved through the GOOS, env lookup and user home dir seams;
  only the state kind is resolved, so the home directory is needed only when the state directory
  itself falls back to it. `deriveFormat` maps `AssetTemplate` and `AssetExt` to the asset format
  (raw, tar.gz or zip), which `Validate` and `Extract` share; `MaxExtractBytes` (default 512 MiB)
  bounds extraction. `CheckWait` (default 250ms, valid 0 to 2s) bounds how long the background check
  delays exit. `Locker` (`filelock.Locker`) defaults to `dirLocker`, which runs
  `os.MkdirAll(StateDir, 0o700)` before locking `StateDir/update.lock`, because filelock refuses a
  missing directory and nothing else creates `StateDir` before the first save. `dirLocker.TryLock`
  does the same before its single attempt; the background check uses `Lock` only.
- `state.go` — `State` (`LastAttempt`, `LatestVersion`), the `StateStore` interface and `fileStore`,
  the default store: `update-state.json` in the state dir through `go.vipengele.org/cli/state`
  (ADR-0023). `Save` creates the directory 0700 and writes the file 0600; a missing or corrupt file
  loads as the zero `State`, an unreadable one is an error. It takes no lock. `StateStore.Load` and
  `Save` honour `ctx`: the background check's bound depends on it. `fileStore` returns `ctx.Err()`
  at once for a done `ctx` and runs its I/O on a goroutine otherwise, abandoning it when `ctx` ends
  (the write is atomic); the unexported `readFile` and `writeFile` fields are the test seam.
- `check.go` — `checkInputs` and `checkInputsFrom` (everything the gates read, through the `Config`
  seams, from a resolved `Config` and the stored `State`), `eligibility` and its `checkReason`: the
  gates in order are dev build, CI, no terminal, disabled, opted out
  (`<EnvPrefix>_NO_UPDATE_CHECK`, via `optOutSuffix`), throttled. `afterAttempt` returns the `State`
  to store: a check stopped by the command ending (`ctx` done or `context.Canceled`) is not an
  attempt and changes nothing; a failed attempt records `LastAttempt` and keeps `LatestVersion`.
- `background.go` — `Config.Start`, `Check` (`Notice`, `Finish`, the unexported `cancelNow`) and
  `runCheck`, the goroutine's body. A nil `*Check` is valid.
- `notice.go` — `Notice` and its one-line `String`.
- `checkhooks.go` — `Config.UpdateCheckHooks`: the pre-run and post-run hooks a tool passes to
  `core.WithPreRun` and `core.WithPostRun`, typed with `spec` types only. The `*Check` travels from
  one to the other in the context, under an unexported key.
- `source.go` — `Source`, `Release` and `Asset`. A nil `Config.Source` resolves to the
  `GitHubSource` (`github.go`) for the Identity's Owner and Repo, using the resolved HTTP client.
- `github.go` — `GitHubSource`: reads the `releases/latest` redirect without following it, falls back
  to the REST API, and with `IncludePrereleases` reads the first page of the release list instead.
  Tags must be plain release versions; `WebURL`, `APIURL` and `Timeout` are overridable for tests.
- `select.go` — `Config.Select` names the archive and checksums assets of a `Release`: the
  `AssetTemplate` and `ChecksumsAsset` rendered with `AssetExt`. A non-nil `Assets` is matched by
  exact name; nil `Assets` (the redirect path, ADR-0019) is located through the `Source`'s optional
  `AssetLocator`, which `GitHubSource.AssetURL` implements.
- `asset/` — the template language (`{name}`, `{version}`, `{tag}`, `{os}`, `{arch}`, `{ext}`) with
  `Validate`, `ValidateChecksums` and `Render`, and `GoReleaserArchive` / `GoReleaserChecksum`, which
  translate a template into a GoReleaser `name_template`; `UsesExt` reports whether a template holds
  `{ext}`. With `{ext}` in the template, `Config.AssetExt` is `tar.gz`, `tgz` or `zip` (`Validate`
  rejects any other), and GoReleaser's archive `format` must produce that extension, or the published
  name and the one `Select` looks for differ. A raw-binary tool leaves `{ext}` out of its template.
- `errors.go` — `StatusError`, `TimeoutError` (a `net.Error`), `TagError`, `AssetNotFoundError`,
  `ChecksumMismatchError`, `ChecksumNotFoundError`, `ChecksumConflictError`, `SizeLimitError`,
  `ManagedInstallError`, `ErrPrerelease` and `ErrAssetsNotListed`, the typed errors a source, `Select` or `Download`
  returns; `classifyError` maps deadline and timeout failures to `TimeoutError` and passes
  cancellation through unchanged. `ExtractLimitError`, `UnsafeEntryError`, `BinaryNotFoundError`,
  `MultipleBinariesError`, `UnsupportedFormatError`, `ErrEmptyAsset`, `ErrNotGzip` and
  `ErrTruncatedGzip` are the errors `Extract` returns. `PermissionError` (unwraps to
  `fs.ErrPermission`, carries a user-facing `Hint`), `BackupLockedError` and `RestoreError` are the
  errors `CheckWritable`, `Install` and `Installed` return.
- `managed.go` — `Config.CheckManaged`, `Detector` and `Install`: resolves the symlinks of the
  executable (an unresolvable path is matched as it is) and returns a `*ManagedInstallError` for
  the first match among the built-in Nix, Homebrew and container detectors, then `Config.Detectors`.
  Prefixes match on a directory boundary, case-sensitively, with "/" separators, so a Windows
  executable never matches; only the default Nix and Homebrew prefixes are known.
- `download.go` — `Config.Download`, `DownloadOption` and `WithProgress`: the checksums request,
  the archive request, and the bounded, hashed copy to the temp file.
- `archive.go` — `Archive`, the verified temp file `Download` returns, and its lifecycle.
- `checksums.go` — `parseChecksum`, which finds the archive's digest in a checksums file.
- `hook.go` — `PostUpdate`, a step that receives the installed binary's path (the old process is still
  running, so `os.Executable` names the wrong file), `Config.PostUpdate` and `RunPostUpdate`, which
  runs the hooks in order and joins their errors. A failure is non-fatal. Package `update` does not
  import `cli/spec/completioninstall` outside tests (ADR-0021); `installscript` is the package that
  does (ADR-0023).
- `installscript/` — `Render(cfg update.Config) ([]byte, error)`: the POSIX-sh install script of a
  tool (`install.sh.in`, embedded), which downloads the release asset, verifies it against the
  checksums file, places the binary with one rename and installs the shell completion.
  - Input: `Render` calls `Config.Resolved` and never re-derives a default. A template holding
    `{ext}` with `AssetExt` `tar.gz` or `tgz` renders the archive path (extract exactly one regular
    file named `BinaryName`); a template without `{ext}` renders the raw-binary path (no `tar`).
    `{ext}` with `zip` fails with `ErrZipAsset`. `{name}` and `{ext}` are fixed at render time;
    `{os}`, `{arch}`, `{tag}` and `{version}` are shell variables expanded when the script runs.
    Every value reaches the script as a single-quoted shell literal.
  - Overrides, each named by `Identity.EnvVar`, so no prefix is spelled out: `<P>_VERSION` (`v1.2.3`
    or `1.2.3`; the latest release when unset), `<P>_INSTALL_DIR` (`$HOME/.local/bin`),
    `<P>_BASE_URL` (`https://github.com`) and `<P>_NO_COMPLETION` (`1` skips the completion).
  - The script source carries `@@NAME@@`-style tokens that one `strings.Replacer` pass fills, so a
    value holding a token stays literal; `@@INSTALL_STEP@@` takes `extract-archive.sh.in` or
    `install-raw.sh.in`. Render inserts `completioninstall.Snippet(BinaryName)` verbatim and never
    copies the completion path table. `completion_target` is called as `if completion_target ...`,
    never bare, because it returns non-zero by design under `set -e`. A completion failure is a
    warning and the exit status stays 0.
  - Not handled: zip, Windows, an API fallback for the latest version (only the `releases/latest`
    redirect is read) and a non-GitHub `Source`. `Config.Source`, `HTTPClient`, the `GOOS` and
    `GOARCH` overrides and the size limits are ignored.
  - Tests run the rendered script under `sh` against `updatetest.NewServer`, with a `PATH` of
    symlinked tools so a test can leave one out; a test skips when a tool the host lacks is needed.
- `extract.go` — `Config.Extract`, `Staged`, the extractor dispatch by format, the raw extractor, the
  bounded copy (`copyBounded`), the context-aware reader and the entry-name check
  (`unsafeEntryName`).
- `extract_tar.go` — the tar.gz extractor: the validating pass, the copy pass, the entry policy
  (`checkTarEntry`) and the cumulative budget reader.
- `extract_zip.go` — the zip extractor and its entry policy (`zipEntryCheck`).
- `target.go` — `Config.Target` (the executable with symlinks resolved, absolute), `Config.CheckWritable`
  (create and remove a temp file in the target's directory), `fsSteps` (the unexported filesystem
  seam: createTemp, remove, chmod, rename, evalSymlinks, stat, abs), `defaultSteps` and `fsError`,
  which maps a permission failure to `*PermissionError`.
- `install.go` — `Config.Install`, `Installed` (`Path`, `Backup`, `Discard`, `Rollback`) and
  `backupLocked`.
- `updatetest/` — the public fake-release kit for tests: an `httptest` server (`NewServer`) serving a
  `Release` in the redirect and REST-API modes, in-memory tar.gz and zip archive (`BuildArchive`) and
  checksums (`Checksums`) builders, and the fault options `BadChecksum`, `MissingAsset`, `Slow` and
  `APIOnly`. It imports neither package `update` (the update tests are `package update`, so that
  would be an import cycle) nor `go.vipengele.org/cli/core`; a test builds its own `GitHubSource`
  from the server's `WebURL`, `APIURL` and `Client`.

## Download contract

- An asset from `Select` on an unlisted release has `Size` 0, which means unknown, not empty.
- The download step maps a 404 on a URL `Select` located to a `*AssetNotFoundError`, not a
  `*StatusError`: a missing asset on the redirect path surfaces there (ADR-0019). `Selection.Located`
  marks such a selection and `Selection.Tag` names the release; any other non-200 is a `*StatusError`.
- `Download` order (ADR-0020): `resolve` the `Config`; reject an empty URL on either asset (an
  unexported sentinel wrapping "update: asset has no URL", not matchable with `errors.Is`); fetch and
  parse the checksums file; stream the archive through a SHA-256 hash into the temp file; compare
  the decoded digests; rewind and return. A missing or conflicting entry fails before the archive
  is requested. The caller never holds the file before it is verified.
- `Archive` exposes `Read`, `ReadAt`, `Seek`, `Size`, `Name` and `Close` only. `Close` closes the
  file before removing it (Windows cannot remove an open file), joins both errors and is idempotent:
  a second call returns nil. Every failure path removes the temp file; a failed removal is joined
  onto the returned error.
- The temp file is `os.CreateTemp(TempDir, TempPrefix+"*")`, mode 0600. A missing `TempDir` is an
  error; nothing creates it.
- Caps: the archive `MaxDownloadBytes` (default 256 MiB), the checksums file 1 MiB, fixed. Both
  give a `*SizeLimitError`, up front from `Content-Length` or a non-zero `Asset.Size`, else while
  streaming. `DownloadTimeout` (default 5 min) is one deadline per request, headers and body; a
  shorter deadline on the context wins and surfaces as a `*TimeoutError`. Cancellation passes
  through unchanged.
- `Size` and `Content-Length` are hints: a body of another length is not an error. The digest
  decides, and a difference is a `*ChecksumMismatchError`.
- Checksums lines are `<64 hex><spaces or '*'><name>`: either hex case, LF or CRLF, a leading `./`
  dropped, spaces kept inside a name; other lines are skipped. The name is matched exactly and
  digests compared as bytes. No entry is a `*ChecksumNotFoundError`; two different digests are a
  `*ChecksumConflictError`.
- `WithProgress` runs synchronously on the caller's goroutine, archive only. `total` is
  `Content-Length`, else a non-zero `Asset.Size`, else -1. The last call, after verification, is
  `(written, written)`; none follows a failure.
- HTTPS is not enforced. Requests use the resolved `HTTPClient` and follow redirects (release
  assets redirect to a CDN), not the redirect-refusing client `GitHubSource` builds for itself.

## Extract contract

- `Extract` order (ADR-0021): reject a nil archive; `resolve` the `Config`; reject an empty `dir`;
  pick the extractor for the derived format; check `ctx`; create the staged file; extract; chmod
  0700; `Sync`; return the `*Staged`. Any failure after the file exists removes it, and a failed
  removal is joined onto the returned error.
- `Extract` owns the `*Archive`: it closes it, removing its temp file, on success and on every failure
  after a non-nil archive, joining a close error onto the result. A close error after a successful
  extraction discards the staged binary and fails.
- `Staged` exposes `Name`, `Keep` (close, leave on disk so a rename can follow) and `Discard` (close,
  then remove). Whichever is called first decides; every later call of either does nothing and
  returns nil. Not safe for concurrent use.
- The staged file is `os.CreateTemp(dir, TempPrefix+"*")`, mode 0700 whatever mode the archive
  records, synced. `dir` is supplied by the caller, has no default and must exist; nothing creates
  it. The caller picks the directory of the file it will rename into place, so the rename stays on
  one filesystem. `TempDir` is for the download only.
- The format is derived from the `Config`, never sniffed from the bytes: a resolved `AssetTemplate`
  without `{ext}` is raw, otherwise `AssetExt` `tar.gz` or `tgz` is tar.gz and `zip` is zip, matched
  exactly in lower case. An empty template or extension counts as `asset.DefaultTemplate` or
  `asset.DefaultExt`. `UnsupportedFormatError` reports a format with no extractor.
- Entry policy: every entry is checked before anything is written, and one unsafe entry (absolute
  path, `..` component, backslash, drive letter, NUL, symlink, hardlink, device, FIFO or any other
  entry that is neither a regular file nor a directory) rejects the whole archive with an
  `*UnsafeEntryError`; entries are never skipped. A zip `zip.ErrInsecurePath` is an `*UnsafeEntryError`
  too. The binary is the regular file whose base name is `BinaryName`, plus `.exe` when the resolved
  `GOOS` is `windows`, at any depth. None is a `*BinaryNotFoundError`, several a
  `*MultipleBinariesError`, and an empty one `ErrEmptyAsset`.
- Budget: `MaxExtractBytes` gives an `*ExtractLimitError`, not a `*SizeLimitError`. For tar.gz one
  cumulative cap covers the whole decompressed stream, headers and skipped bodies included, because
  `tar.Reader.Next` reads through the bodies before it. For zip and raw the cap applies to the
  selected entry or file alone and counts bytes actually read, never the declared size; the other
  zip entries are never read.
- tar.gz is read twice: a validating pass over every entry and the gzip trailer, then the copy of
  the binary's body, so nothing is written for a rejected archive. A non-gzip input is `ErrNotGzip`
  and a stream that ends early `ErrTruncatedGzip`. A non-zip or truncated zip surfaces as a wrapped
  `zip.ErrFormat`.
- `ctx` is checked before each entry and between reads; a cancelled `ctx` returns an error matching
  `context.Canceled`.

## Install contract

- `Install` order (ADR-0022): reject a nil `*Staged`; `resolve` the `Config`; resolve the target
  (`Executable`, `filepath.EvalSymlinks`, `filepath.Abs`); stat it for its mode; check the staged
  file's directory is the target's; `Keep` the staged file; chmod it by path to the target's
  `Mode().Perm()` (setuid, setgid and sticky are dropped); rename the target to `<target>.old`;
  rename the staged file to the target. Nothing at the target changes before the first rename.
- `Install` owns the `*Staged`. On every failure the staged file is removed by path and a failed
  removal is joined onto the error. When the rename into place fails the backup is renamed back
  first; if that fails too the error is a `*RestoreError` carrying the backup path.
- **The two renames are load-bearing.** Windows lets a running `.exe` be renamed but not
  overwritten or deleted, so the old binary must move aside before the new one moves in. Never
  collapse them into one rename over the target, or into a copy onto it.
- `Installed` is in memory only. A backup orphaned by a crash is invisible to the API; the next
  `Install` overwrites it. `Discard` and `Rollback` settle once: the first call decides and later
  calls of either return nil.
- Only the resolved path is replaced. A symlink to it follows the new binary; another hard link to
  the same file keeps the old one.
- `Discard` returns `*BackupLockedError` only when the resolved `Config.GOOS` is `windows` and the
  remove error unwraps to errno 5 (access denied) or 32 (sharing violation). Access denied matches
  too, so an ACL denial on Windows reads as locked. The update has succeeded, and a caller may ignore
  it. Any other remove failure is a `*PermissionError` or a wrapped error.
- `*PermissionError` comes from `CheckWritable` and from any write step of `Install`, `Discard` and
  `Rollback`. `CheckWritable` is best-effort; `Install` is the authority.
- Windows behaviour is unverified: one code path, no `_windows.go` file, so the `GOOS=windows go
  vet` gate stays off for this project (ADR-0022). The injected-error tests cover the locked-backup
  branch.
- The seams are the unexported `fsSteps`, set by in-package tests; `Config` has no field for them.
  Tests run as root in CI, so injection is what covers the permission path.
- `Install` runs no `PostUpdate` hook. The caller runs `RunPostUpdate` with `Installed.Path()`.

## Background check contract

- The goroutine runs on `context.WithoutCancel(ctx)` with a cancel of its own, so the command's
  cancellation does not reach it; only `Finish` and `cancelNow` do.
- `Finish` is the only wait. It is bounded by `CheckWait` only while `Locker`, `Source` and
  `StateStore` honour `ctx`, because it cancels and then joins the goroutine.
- The check takes the lock, reloads the state and re-throttles under it. When another process has
  just checked, its `LatestVersion` is this run's result: no query and no save. A reload error
  abandons the check.
- A cancelled check is not an attempt and saves nothing (`afterAttempt`).
- Nothing is printed but the notice, and it is written only when stderr is a terminal. A failed
  lock, query or save is never reported and never fails the command.
- The post-run hook always returns nil, so the run's error passes through. It finishes the check
  before it reads the notice, and cancels at once when the command's context is done. It does not
  probe the terminal: the pre-run hook's `Start` decides that gate once, from `Config.IsTerminal`
  when set and the Env's stderr otherwise, and a run that fails it has no `*Check`.
- A command carrying `spec.SkipUpdateCheck` starts no check; the tool's `update` command must carry
  it.

## Conventions

- Reach the outside world only through a `Config` field, never through `http.DefaultClient`,
  `time.Now`, `runtime.GOOS`, `os.Getenv` and the like called directly. A new seam is a new `Config`
  field with a documented zero-value default, resolved in `resolve`; there are two exceptions. One
  is `Install`'s unexported `fsSteps` (see the Install contract), which only in-package tests set.
  The other is the `CheckWait` bound in `Finish`, a `time.NewTimer`: it is wall-clock time, while
  `Now` stamps state only.
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- `depguard` forbids importing `go.vipengele.org/cli/core/**` anywhere, and `github.com/spf13/cobra`
  outside an `updatecmd` package. The library depends on no command-line framework.
- `cli/spec`, `cli/state`, `commons` and `filelock` are siblings reached by directory `replace`s in
  `go.mod` (`../cli/spec`, `../cli/state`, `../commons`, `../filelock`); see the repo-root rule on
  sibling replaces.

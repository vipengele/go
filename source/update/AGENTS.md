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
  home dir, `EvalSymlinks`, `InContainer`, `IsTerminal`, `CheckWait`, `Locker`, `Token`) and the tool's own
  `Detectors`; a nil `Detectors` entry is invalid. The signature policy is required (ADR-0024,
  ADR-0026): `TrustedKeys` (minisign public key entries, each with or without its
  `untrusted comment:` line, several for a rotation) or `AllowUnsigned`, exactly one. `Validate`
  rejects an entry that does not parse, a repeated key id, neither field and both fields, so
  `update.Config{}` is invalid for every operation, `Check` and `CheckManaged` included, because
  `resolve` runs `Validate`. A test `Config` sets `TrustedKeys: []string{srv.PublicKey()}`.
  `Validate` reports problems; `resolve` returns a copy with each zero field set to its production
  default and never mutates the receiver. `Resolved` is its exported wrapper, the one place other
  packages read the defaults from (ADR-0023). The default state dir comes from
  `paths.Resolve(..., paths.State)` in `go.vipengele.org/cli/spec/paths`, resolved through the GOOS,
  env lookup and user home dir seams; only the state kind is resolved, so the home directory is
  needed only when the state directory itself falls back to it. `deriveFormat` maps `AssetTemplate`
  and `AssetExt` to the asset format (raw, tar.gz or zip), which `Validate` and `Extract` share;
  an empty `AssetExt` is `asset.DefaultExtFor` the resolved `GOOS` (`zip` on `windows`, `tar.gz`
  elsewhere), which `resolve` fills after `GOOS`; `MaxExtractBytes` (default 512 MiB) bounds extraction. `CheckWait` (default 250ms, valid 0 to 2s)
  bounds how long the background check delays exit. `Channel` (default `Stable`, valid when one of
  `Stable`, `Beta` and `Edge`) is the release channel the update follows; a tool maps its own config
  key onto it through `ParseChannel`. The default `InContainer` is `paths.InContainerFor(Identity, ...)`:
  `<EnvPrefix>_CONTAINER` decides when set (a boolean; any other non-empty value is true), else the
  `paths.InContainer` heuristic. `Locker` (`filelock.Locker`) defaults to
  `dirLocker`, which runs `os.MkdirAll(StateDir, 0o700)` before locking `StateDir/update.lock`,
  because filelock refuses a missing directory and nothing else creates `StateDir` before the first
  save. `dirLocker.TryLock` does the same before its single attempt; the background check uses
  `Lock` only.
- `channel.go` — `Channel` (`Stable < Beta < Edge`, cumulative, zero is `Stable`) with `String`,
  `Receives`, `MarshalText`/`UnmarshalText`, and `ParseChannel` (exactly `stable`, `beta`, `edge`).
  `channelOf(tag, prerelease)` is the higher of the tag's channel (no suffix: `Stable`; first suffix
  identifier `beta` or `rc`: `Beta`; any other suffix: `Edge`) and the flag's (`prerelease: true` is
  at least `Beta`); it assumes a release version. Ordering is semver (`version.Newer`) on every
  channel (ADR-0027).
- `downgrade.go` — `Config.CheckDowngrade(ctx, tag)` and `downgradeFloor`. The floor is the higher of
  `State.HighestApplied` and the running `Version`, each ignored when not a release version. A tag
  strictly below it is a `*DowngradeError`; equal passes; a tag that is not a release version is
  below every floor. It fails closed: an invalid `Config` or a `StateStore.Load` error is returned
  as it is, never guessed around.
- `state.go` — `State` (four keys, each omitted when zero: `last_attempt`, `latest_version`,
  `latest_channel`, `highest_applied`; a reader ignores unknown keys, so an older build rewriting the
  file drops `highest_applied`), `lastAttemptOn`/`latestVersionOn` (a value learnt on another
  channel reads as empty), the `StateStore` interface and `fileStore`,
  the default store: `update-state.json` in the state dir through `go.vipengele.org/cli/state`
  (ADR-0023). `Save` creates the directory 0700 and writes the file 0600; a missing or corrupt file
  loads as the zero `State`, an unreadable one is an error. It takes no lock. `StateStore.Load` and
  `Save` honour `ctx`: the background check's bound depends on it. `fileStore` returns `ctx.Err()`
  at once for a done `ctx` and runs its I/O on a goroutine otherwise, abandoning it when `ctx` ends
  (the write is atomic); the unexported `readFile` and `writeFile` fields are the test seam.
- `check.go` — `checkInputs` and `checkInputsFrom` (everything the gates read, through the `Config`
  seams, from a resolved `Config` and the stored `State`), `eligibility` and its `checkReason`: the
  gates in order are dev build, CI, no terminal, disabled, opted out
  (`<EnvPrefix>_NO_UPDATE_CHECK`, via `optOutSuffix`), throttled; `LastAttempt` is read per channel,
  so a state learnt on another channel never throttles. `afterAttempt` returns the `State` to store:
  a check stopped by the command ending (`ctx` done or `context.Canceled`) is not an attempt and
  changes nothing; any other attempt records `LastAttempt` and `LatestChannel`; success sets
  `LatestVersion`; a failure keeps `LatestVersion` when the state was learnt on the same channel and
  clears it when it was learnt on another (otherwise an unreachable source is queried on every run
  after a switch). `HighestApplied` is never touched.
- `background.go` — `Config.Start`, `Check` (`Notice`, `Finish`, the unexported `cancelNow`) and
  `runCheck`, the goroutine's body. A nil `*Check` is valid. `Notice` never names a release below the
  `HighestApplied` the state held at `Start`. When the check leaves a release to announce, the
  goroutine runs `CheckManaged` after `runCheck`; `Start` itself does no executable or container I/O.
  `CheckManaged` takes no context, so `managedInsteadWithin` runs it on a goroutine of its own and
  waits only until the check's context ends (skipping it when that context is already done); an
  abandoned managed check leaves `Instead` empty and delivers into a one-slot buffer, so it never
  blocks `Finish` or leaks blocked on a send. A `*ManagedInstallError` sets the notice's `Instead`,
  any other error is ignored, and `Instead` is read only once the goroutine has finished.
- `notice.go` — `Notice` and its one-line `String`: the update command, or, with `Instead` set, the
  managed install's instruction in its place.
- `checkhooks.go` — `Config.UpdateCheckHooks`: the pre-run and post-run hooks a tool passes to
  `core.WithPreRun` and `core.WithPostRun`, typed with `spec` types only. The `*Check` travels from
  one to the other in the context, under an unexported key. The pre-run hook first calls
  `removeStaleBackupOnHost(runtime.GOOS)`, which removes a stale backup on a Windows host only.
- `stale.go` — `Config.RemoveStaleBackup` (removes `<Target>.old`; none is nil; a held-open backup
  is a `*BackupLockedError` and stays) and `removeStaleBackupOnHost`, which does nothing unless the
  host GOOS it is given is `windows` (ADR-0031).
- `source.go` — `Source`, `Release` and `Asset`. A nil `Config.Source` resolves to the
  `GitHubSource` (`github.go`) for the Identity's Owner and Repo, using the resolved HTTP client.
  `Source.Latest(ctx, ch)` takes the channel; `Release` carries none.
- `github.go` — `GitHubSource`: on `Stable` it reads the `releases/latest` redirect without following
  it and falls back to the REST API. On `Beta` and `Edge` it reads the release list page by page
  (100 a page, newest-created first) until a page holds a release the channel receives, a page is
  short or empty, or 10 pages are read, and returns the highest receivable release across the pages
  read; drafts and non-release tags are skipped, and none within the bound is an error. Stable tags
  must be plain release versions; `WebURL`, `APIURL`, `Timeout` and `Now` are overridable for tests.
  `Token` (`Config.Token`; nil reads `GH_TOKEN`, then `GITHUB_TOKEN`, through `Getenv`; `""` opts
  out; a provider error fails the API request, wrapped) authenticates REST API requests only
  (ADR-0028). `latestByAPI` and `highestListed` each build the API client once from the injected one
  with `apiClient` (`githubauth.go`), which reads the token, and pass it to `apiGet` for every
  request, every list page included, so a `Latest` reads the token at most once and only for an API
  request. The wrapping transport adds `Authorization: Bearer` only when the request host equals the
  `APIURL` host (case-insensitive, port included), on a clone of the request, and `CheckRedirect`
  refuses a cross-host redirect with a `*CrossHostRedirectError`, declared in `githubauth.go`.
  Downloads (`Config.HTTPClient`) and the web redirect probe never send the token. Every REST API
  response goes through `apiStatusError`; a 401 to an authenticated request is a
  `*TokenRejectedError`, never an unauthenticated retry.
- `githubcache.go` — `ResponseCache` (`Get`, `Put`), `CachedResponse` (`ETag`, `Body`) and
  `NewFileResponseCache`, the default of the default source: `github-cache.json` in `StateDir`
  through `go.vipengele.org/cli/state` (0700/0600, atomic), kept apart from `State` so a custom
  `StateStore` needs no new fields. Its I/O is bounded by `ctx` in `fileStore`'s goroutine-and-select
  shape. Entries are keyed by URL, so each list page has its own. `GitHubSource` sends
  `If-None-Match` only when an entry exists, decodes a 304 through the path a 200 takes, replaces the
  entry on a 200 with an ETag (bodies up to the read caps, 8 MiB for latest and 32 MiB for a list
  page), treats a failed load as a miss and a failed save as a skipped write, writes nothing once
  `ctx` is done, and never returns the cached body for an error response. An unauthenticated 304 is
  not exempt from the rate limit: it saves transfer, not quota.
- `select.go` — `Config.Select` names the archive and checksums assets of a `Release`: the
  `AssetTemplate` and `ChecksumsAsset` rendered with `AssetExt`. A non-nil `Assets` is matched by
  exact name; nil `Assets` (the redirect path, ADR-0019) is located through the `Source`'s optional
  `AssetLocator`, which `GitHubSource.AssetURL` implements. `Selection.Signature` is the checksums
  asset's name plus `.minisig`, or the zero `Asset` under `AllowUnsigned`; a `Selection` lists
  exactly what `Download` reads, and a listed release without the signature asset is an
  `*AssetNotFoundError`.
- `asset/` — the template language (`{name}`, `{version}`, `{tag}`, `{os}`, `{arch}`, `{ext}`) with
  `Validate`, `ValidateChecksums` and `Render`, and `GoReleaserArchive` / `GoReleaserChecksum`, which
  translate a template into a GoReleaser `name_template`; `UsesExt` reports whether a template holds
  `{ext}`. With `{ext}` in the template, `Config.AssetExt` is `tar.gz`, `tgz` or `zip` (`Validate`
  rejects any other), and GoReleaser's archive `format` must produce that extension, or the published
  name and the one `Select` looks for differ. A raw-binary tool leaves `{ext}` out of its template.
  `DefaultExtFor(goos)` is the one source of the default extension: `zip` for `windows`, `tar.gz`
  for every other GOOS (ADR-0031); a non-empty `AssetExt` is used on every GOOS.
- `errors.go` — `StatusError`, `RateLimitError` (a 403 or 429 from the REST API with
  `X-RateLimit-Remaining: 0` or a `Retry-After`; carries `Reset`, `Limit`, `Remaining`,
  `Authenticated`, `Status`; its text names the reset in RFC 3339 UTC and reads no clock),
  `TokenRejectedError` (a 401 to an authenticated request; both unwrap to the `*StatusError`, and
  neither is produced by a download), `TimeoutError` (a `net.Error`), `TagError`,
  `AssetNotFoundError`, `ChecksumMismatchError`, `ChecksumNotFoundError`, `ChecksumConflictError`,
  `SizeLimitError`, `SignatureMalformedError` (unwraps to the minisign parse error),
  `UnknownKeyError` (carries the signature's key id as minisign prints it),
  `SignatureMismatchError` (carries the key id, unwraps to the minisign verification error),
  `ManagedInstallError`, `ErrPrerelease` (the `Stable` channel's latest release is a pre-release)
  and `ErrAssetsNotListed`, the typed errors a source, `Select` or `Download` returns;
  `DevBuildError`, the error `CheckRelease` returns; `DowngradeError` (`Name`, `Version`, `Floor`),
  the error `CheckDowngrade` returns. `classifyError` maps deadline and timeout failures to
  `TimeoutError` and passes cancellation through unchanged. `ExtractLimitError`,
  `UnsafeEntryError`, `BinaryNotFoundError`, `MultipleBinariesError`, `UnsupportedFormatError`,
  `ErrEmptyAsset`, `ErrNotGzip` and `ErrTruncatedGzip` are the errors `Extract` returns.
  `PreflightError` (`Path`, `Reason`, `Stderr`, `Err`; unwraps to `Err`) is the error `CheckStaged`
  returns. `PermissionError` (unwraps to
  `fs.ErrPermission`, carries a user-facing `Hint`), `BackupLockedError` and `RestoreError` are the
  errors `CheckWritable`, `Install` and `Installed` return.
- `managed.go` — `Config.CheckManaged`, `Detector` and `Install`: resolves the symlinks of the
  executable (an unresolvable path is matched as it is) and returns a `*ManagedInstallError` for
  the first match among the built-in Nix, Homebrew and container detectors, then `Config.Detectors`.
  Prefixes match on a directory boundary, case-sensitively, with "/" separators, so a Windows
  executable never matches; only the default Nix and Homebrew prefixes are known.
- `download.go` — `Config.Download`, `DownloadOption` and `WithProgress`: the buffered checksums
  and signature requests (`fetchBounded`), the signature check (`verifySignature`, which maps the
  `internal/minisign` errors to the public ones in `signatureError`), the archive request, and the
  bounded, hashed copy to the temp file.
- `archive.go` — `Archive`, the verified temp file `Download` returns, and its lifecycle.
- `checksums.go` — `parseChecksum`, which finds the archive's digest in a checksums file.
- `hook.go` — `PostUpdate`, a step that receives the installed binary's path (the old process is still
  running, so `os.Executable` names the wrong file), `Config.PostUpdate` and `RunPostUpdate`, which
  runs the hooks in order and joins their errors. A failure is report-only: the caller keeps the
  update. `Install` and `Apply` run no hook. Package `update` does not import
  `cli/spec/completioninstall` outside tests (ADR-0021); `installscript` is the package that does
  (ADR-0023).
- `installscript/` — `Render(cfg update.Config) ([]byte, error)`: the POSIX-sh install script of a
  tool (`install.sh.in`, embedded), which downloads the release asset, verifies it against the
  checksums file (and, with `TrustedKeys`, the checksums file against its minisign signature),
  places the binary with one rename and installs the shell completion. `RenderPowerShell` is the
  same install for Windows (`install.ps1.in`, ADR-0031).
  - Input: `Render` calls `Config.Resolved` and never re-derives a default. Each renderer resolves
    for its target: `Render` sets `GOOS` `linux` and `RenderPowerShell` `windows` on a copy when the
    `Config` names none, so the host never decides a default; a `Config` naming `windows` resolves
    `AssetExt` to `zip` and has no sh script. A template holding `{ext}` with `AssetExt` `tar.gz` or
    `tgz` renders the archive path (extract exactly one regular file named `BinaryName`); a template
    without `{ext}` renders the raw-binary path (no `tar`).
    `{ext}` with `zip` fails with `ErrZipAsset`. `{name}` and `{ext}` are fixed at render time;
    `{os}`, `{arch}`, `{tag}` and `{version}` are shell variables expanded when the script runs.
    Every value reaches the script as a single-quoted shell literal.
  - Overrides, each named by `Identity.EnvVar`, so no prefix is spelled out: `<P>_VERSION` (`v1.2.3`
    or `1.2.3`; the latest release when unset), `<P>_INSTALL_DIR` (`$HOME/.local/bin`),
    `<P>_BASE_URL` (`https://github.com`), `<P>_NO_COMPLETION` (`1` skips the completion) and, in a
    script rendered with `TrustedKeys`, `<P>_ALLOW_UNSIGNED` (`1` installs on the checksum alone
    when `minisign` is not on `PATH`; no effect when it is).
  - Signature (ADR-0026): `Render` fails on a `Config` that `Validate` rejects. Under
    `AllowUnsigned` the script is the one with no signature check at all: the lines between
    `@@SIGNED_BEGIN@@` and `@@SIGNED_END@@` in `install.sh.in` are dropped, markers included, before
    the tokens are filled. With keys they are kept: after the checksums file the script fetches
    `<checksums>.minisig` (a 404 fails), then runs `minisign -V -q -P <key>` once per key and fails
    unless one verifies. Each key reaches the script as `minisign.ParsePublicKey(entry).Line`,
    single-quoted, never the raw entry. Without `minisign` it warns, then proceeds only on
    `<P>_ALLOW_UNSIGNED=1` or a `y`/`yes` (any case) read from `/dev/tty`; it treats `/dev/tty` as
    usable only when `(: </dev/tty)` succeeds, and with no terminal it fails naming both remedies.
    Every refusal happens before the install directory is touched.
  - The script source carries `@@NAME@@`-style tokens that one `strings.Replacer` pass fills, so a
    value holding a token stays literal; `@@INSTALL_STEP@@` takes `extract-archive.sh.in` or
    `install-raw.sh.in`. Render inserts `completioninstall.Snippet(BinaryName)` verbatim and never
    copies the completion path table. `completion_target` is called as `if completion_target ...`,
    never bare, because it returns non-zero by design under `set -e`. A completion failure is a
    warning and the exit status stays 0.
  - PowerShell (`RenderPowerShell`): the sh script's releases, checksums, signature policy and
    variables, for Windows PowerShell 5.1 and PowerShell 7, saved or run as `irm <url> | iex`.
    `@@INSTALL_STEP@@` takes `extract-zip.ps1.in` (`Expand-Archive`), `extract-tar.ps1.in`
    (`tar.exe`, Windows 10 and later) or `install-raw.ps1.in`; every value is a single-quoted
    PowerShell literal (`powerShellQuote`, which doubles the typographic quotes too). The default
    install directory is `%LOCALAPPDATA%\Programs\<name>`. An existing `<name>.exe` is moved to
    `<name>.exe.old` before the staged file moves in; the `.old` is removed at once when nothing runs
    it, and otherwise by the next install. When the install directory is not on PATH it is appended
    to the raw `HKCU\Environment\Path` (written back with its own kind, `REG_EXPAND_SZ` kept),
    `WM_SETTINGCHANGE` is broadcast and the session's PATH gains it; `<P>_NO_MODIFY_PATH=1`
    replaces that with a warning. The sh script only warns. The completion is
    `completioninstall.PowerShellSnippet(BinaryName)`, verbatim, skipped on `<P>_NO_COMPLETION=1`;
    `$PROFILE` is never edited. Its tests (`script_windows_test.go`) run on the Windows CI job.
  - Not handled: zip in the sh script (`ErrZipAsset`; Windows uses `RenderPowerShell`), an API
    fallback for the latest version (only the `releases/latest` redirect is read) and a non-GitHub
    `Source`. `Config.Source`, `HTTPClient`, the `GOARCH` override and the size limits are ignored;
    `GOOS` decides only the defaults.
  - Tests run the rendered script under `sh` against `updatetest.NewServer`, with a `PATH` of
    symlinked tools so a test can leave one out; a test skips when a tool the host lacks is needed.
    `minisign` is never the host's: `TestMain` builds a stand-in from `testdata/fakeminisign` and
    the harness links it onto `PATH` unless `NoMinisign`, so no test needs minisign installed. The
    `TTY` option gives the script a pty as controlling terminal and stderr (linux only; elsewhere
    the test skips), with input from `TTYInput`/`TTYEOF` and output in `runResult.TTY`; without it
    the script runs with no controlling terminal, so opening `/dev/tty` fails. The harness renders
    the script before the server's key exists, so a test of a keyed script re-renders it with
    `Server.PublicKey()` (`newSignedHarness`).
- `internal/minisign/` — the minisign verifier: `ParsePublicKey` (with or without the `untrusted
  comment:` line; `.Line` is the bare key line), `ParseSignature` and `Verify`, accepting both the
  hashed `ED` and the legacy `Ed` signature forms. Its `testdata/` holds an interop vector (a key and
  signatures from an independent implementation; see its `README.md`). `internal/minisigntest` is the signer for tests only
  (`GenerateKey`, `Sign`, `SignLegacy`); production code never imports it.
- `extract.go` — `Config.Extract`, `Staged`, the extractor dispatch by format, the raw extractor, the
  bounded copy (`copyBounded`), the context-aware reader and the entry-name check
  (`unsafeEntryName`).
- `extract_tar.go` — the tar.gz extractor: the validating pass, the copy pass, the entry policy
  (`checkTarEntry`) and the cumulative budget reader.
- `extract_zip.go` — the zip extractor and its entry policy (`zipEntryCheck`).
- `target.go` — `Config.Target` (the executable with symlinks resolved, absolute), `Config.CheckWritable`
  (create and remove a temp file in the target's directory), `fsSteps` (the unexported filesystem
  seam: createTemp, remove, chmod, rename, evalSymlinks, stat, abs, run), `defaultSteps` and `fsError`,
  which maps a permission failure to `*PermissionError`.
- `install.go` — `Config.Install`, `Installed` (`Path`, `Backup`, `Discard`, `Rollback`) and
  `backupLocked`. `install_windows_test.go` replaces a running executable on Windows.
- `release.go` — `Config.CheckRelease`: refuses a development build with a `*DevBuildError`.
- `preflight.go` — `Preflight` (a tool's hook over a `StagedBinary`; an error vetoes the install),
  `StagedBinary` and `Config.CheckStaged(ctx, s, sel)`: runs the staged binary with `version --json`
  and refuses it unless the answer is the selected release for the resolved GOOS and GOARCH, then
  runs each `Config.Preflight` hook in order. `Config.PreflightTimeout` (default 10s, not negative)
  kills the run. The binary's standard output is capped at 64 KiB. Every refusal is a
  `*PreflightError`; a done `ctx` returns its own error. It closes the staged file's write
  descriptor (a file open for writing cannot be run) and neither keeps nor discards the `*Staged`.
- `apply.go` — `Config.Apply(ctx, sel, opts...)`, nine steps stopping at the first error: 1
  CheckManaged, 2 CheckWritable, 3 CheckDowngrade of `sel.Tag` (skipped, with its State read, by
  `AllowDowngrade()`), 4 Download, 5 Target, 6 Extract into the target's directory, 7 CheckStaged
  (a refusal discards the staged file), 8 Install, 9 the record (`recordApplied`): under the
  Locker, load the State, raise `HighestApplied` to the tag when newer, save the whole State. The
  guard fails closed (an unreadable State refuses the install); the record fails open (a failed
  lock, load or save is ignored, since the new binary's `Version` floors the next run). Steps 1 to 3
  make no request. It returns the `*Installed` with the backup kept, runs no hook and calls neither
  `Discard` nor `Rollback`. It closes the archive itself only when Target fails.
  `WithDownloadOptions` passes options to Download.
- `updatecmd/` — `Commands(cfg, opts...) []*spec.Command`: one `update` command with `--check`,
  `--yes`/`-y`, `--to`, `--allow-downgrade` and `--channel`, carrying `spec.SkipUpdateCheck`.
  - `ExitUpdateAvailable = 10` is the exit code of `--check` when a newer release exists; the
    result goes to stdout, messages to stderr.
  - A development build is refused in both modes. `CheckManaged` runs first: in `--check` a
    `*ManagedInstallError` only adds a hint to the line; in an update it is returned before any
    request.
  - `--to` accepts `1.2.3` or `v1.2.3`; a version equal to the current one is a no-op. `--to`
    combined with `--check`, or not a release version, is a plain error (exit 1). `--to` alone does
    not lift the downgrade guard: a tag below the floor is a `*DowngradeError` unless
    `--allow-downgrade` is given (which passes `update.AllowDowngrade()` to `Apply`).
  - `--channel stable|beta|edge` replaces `Config.Channel` for one run through `ParseChannel`; any
    other value is a plain error.
  - The floor decides whether there is an update: the guard runs before the confirmation on the
    latest path and the `--to` path, and `--check` reports up to date for a latest below the floor.
  - Without `--yes` it asks with `Confirm(Required: true)`, so a non-terminal stdin is the
    framework's `*spec.UsageError` (exit 2).
  - After `Apply`, a `RunPostUpdate` failure and a `Discard` failure (naming the backup path) are
    warnings and the exit stays 0.
- `updatetest/` — the public fake-release kit for tests: an `httptest` server (`NewServer`) serving a
  `Release` in the redirect and REST-API modes, the release binary `NewRelease` packs
  (`ReleaseExecutable`: the shell script `ReleaseBinary` off Windows, a Go program built at test
  time on Windows), in-memory tar.gz and zip archive (`BuildArchive`) and
  checksums (`Checksums`) builders, and the fault options `BadChecksum`, `BadSignature`,
  `UnknownKey`, `Tampered`, `MissingAsset`, `Slow` and `APIOnly`. The server signs the checksums
  file with a key generated per server (`internal/minisigntest`), serves it as
  `<ChecksumsName>.minisig` (`TargetSignature`) and returns the trusted entry from `PublicKey`. It imports neither package `update` (the update tests are `package update`, so that
  would be an import cycle) nor `go.vipengele.org/cli/core`; a test builds its own `GitHubSource`
  from the server's `WebURL`, `APIURL` and `Client`.

## Download contract

- An asset from `Select` on an unlisted release has `Size` 0, which means unknown, not empty.
- The download step maps a 404 on a URL `Select` located to a `*AssetNotFoundError`, not a
  `*StatusError`: a missing asset on the redirect path surfaces there (ADR-0019). `Selection.Located`
  marks such a selection and `Selection.Tag` names the release; any other non-200 is a `*StatusError`.
- `Download` order (ADR-0020, ADR-0024, ADR-0026): `resolve` the `Config`; reject an empty URL on
  the archive, the checksums file and, unless `AllowUnsigned`, the signature (an unexported sentinel
  wrapping "update: asset has no URL", not matchable with `errors.Is`; a zero `Signature` is named
  `<checksums>.minisig`); read the checksums file into memory; unless `AllowUnsigned`, fetch the
  signature and verify it over those bytes with the parsed `TrustedKeys`; parse the digest; stream
  the archive through a SHA-256 hash into the temp file; compare the decoded digests; rewind and
  return. Trust is established before the checksums file is parsed and before the archive is
  requested: a malformed signature (`*SignatureMalformedError`), an unknown key id
  (`*UnknownKeyError`), a failed verification (`*SignatureMismatchError`), a missing signature, and a
  missing or conflicting entry each fail with no archive request and nothing staged. Under
  `AllowUnsigned` no signature is requested, whatever `Selection.Signature` holds. The caller never
  holds the file before it is verified.
- `Archive` exposes `Read`, `ReadAt`, `Seek`, `Size`, `Name` and `Close` only. `Close` closes the
  file before removing it (Windows cannot remove an open file), joins both errors and is idempotent:
  a second call returns nil. Every failure path removes the temp file; a failed removal is joined
  onto the returned error.
- The temp file is `os.CreateTemp(TempDir, TempPrefix+"*")`, mode 0600. A missing `TempDir` is an
  error; nothing creates it.
- Caps: the archive `MaxDownloadBytes` (default 256 MiB), the checksums file 1 MiB and the signature
  4 KiB (`maxSignatureBytes`), the last two fixed. Each gives a `*SizeLimitError`, up front from
  `Content-Length` or a non-zero `Asset.Size`, else while streaming. `DownloadTimeout` (default 5 min) is one deadline per request, headers and body; a
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
- HTTPS is not enforced: with keys the signature authenticates the checksums file and the digest
  carries that to the archive; under `AllowUnsigned` the checksums file is trusted as it arrives.
  Requests use the resolved `HTTPClient` and follow redirects (release assets redirect to a CDN), not the redirect-refusing client `GitHubSource` builds for itself.

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
- The staged file is `os.CreateTemp(dir, TempPrefix+"*"+ext)`, where `ext` is the extension of the
  binary's file name (`.exe` for a Windows target, empty otherwise): the name is `TempPrefix`, a
  random part and `ext`, so a staged Windows binary can be run. Mode 0700 whatever mode the archive
  records, synced. `dir` is supplied by the caller, has no default and must exist; nothing creates
  it. The caller picks the directory of the file it will rename into place, so the rename stays on
  one filesystem. `TempDir` is for the download only.
- The format is derived from the `Config`, never sniffed from the bytes: a resolved `AssetTemplate`
  without `{ext}` is raw, otherwise `AssetExt` `tar.gz` or `tgz` is tar.gz and `zip` is zip, matched
  exactly in lower case. An empty template counts as `asset.DefaultTemplate` and an empty extension
  as `asset.DefaultExtFor` the `GOOS` (`runtime.GOOS` when that is empty too). `UnsupportedFormatError` reports a format with no extractor.
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
- On Windows the backup of the running executable outlives the process that ran the update:
  `Discard` returns `*BackupLockedError` and `<target>.old` stays until `RemoveStaleBackup`, which
  the `UpdateCheckHooks` pre-run hook calls on every command of a Windows host, removes it after
  the process exits. Off Windows the hook removes none: an open file can be removed there, so a
  removal could take the backup an unsettled update's `Rollback` needs, and `Discard` always removes
  the backup, so only a crash leaves one, which the next `Install` replaces (ADR-0031).
  `RemoveStaleBackup` itself removes on every OS; never call it between `Install` and the settling
  `Discard` or `Rollback`.
- `*PermissionError` comes from `CheckWritable` and from any write step of `Install`, `Discard` and
  `Rollback`. `CheckWritable` is best-effort; `Install` is the authority.
- One code path for every OS, no `_windows.go` file. Windows behaviour is tested on the gated
  `test-windows` job (ADR-0030): `install_windows_test.go` installs over a running executable in a
  path holding a space and checks `Discard` and `RemoveStaleBackup` report it locked until it exits.
  The project's `*_windows_test.go` files also turn on `GOOS=windows go vet` in `ci-build.yml` and
  `release.yml`. The injected-error tests cover the locked-backup branch on Linux, where coverage
  is measured.
  The Windows job runs the preflight's real execution of a staged `.exe`: the release binary
  `updatetest.NewRelease` packs on Windows is a Go program (`updatetest.ReleaseExecutable`), so
  `TestApplyRunsTheStagedBinary` and `updatecmd`'s install tests run there too.
- Limits (ADR-0031): an antivirus or indexer lock can fail a rename, and then the executable is
  untouched or the backup is renamed back and the user retries, or it keeps `.old` from being
  removed, and the next run removes it; there is no retry loop. Paths with spaces are supported.
  The sh installer refuses zip, so Windows uses the PowerShell installer, which needs Windows
  PowerShell 5.1 or later, and `tar.exe` (Windows 10 and later) for a tar.gz asset.
- The seams are the unexported `fsSteps`, set by in-package tests; `Config` has no field for them.
  Tests run as root in CI, so injection is what covers the permission path.
- `Install` and `Apply` run no `PostUpdate` hook. The caller runs `RunPostUpdate` with
  `Installed.Path()`; a hook failure is report-only and the caller keeps the update.

## Background check contract

- The goroutine runs on `context.WithoutCancel(ctx)` with a cancel of its own, so the command's
  cancellation does not reach it; only `Finish` and `cancelNow` do.
- `Finish` is the only wait. It is bounded by `CheckWait` only while `Locker`, `Source` and
  `StateStore` honour `ctx`, because it cancels and then joins the goroutine.
- The check takes the lock, reloads the state and re-throttles under it. When another process has
  just checked on this channel, its `LatestVersion` is this run's result: no query and no save. A reload error
  abandons the check.
- A cancelled check is not an attempt and saves nothing (`afterAttempt`).
- The throttle is per channel: `Start` and the reload under the lock read `LastAttempt` and
  `LatestVersion` through `lastAttemptOn` and `latestVersionOn` for `Config.Channel`, so a state
  learnt on another channel counts the throttle as elapsed and announces nothing.
- Nothing is printed but the notice, and it is written only when stderr is a terminal. A failed
  lock, query or save is never reported and never fails the command.
- The post-run hook always returns nil, so the run's error passes through. It finishes the check
  before it reads the notice, and cancels at once when the command's context is done. It does not
  probe the terminal: the pre-run hook's `Start` decides that gate once, from `Config.IsTerminal`
  when set and the Env's stderr otherwise, and a run that fails it has no `*Check`.
- A command carrying `spec.SkipUpdateCheck` starts no check; `updatecmd`'s `update` command
  carries it.
- `Start` does no executable I/O. On a Windows host the pre-run hook calls `RemoveStaleBackup`
  before it looks at `SkipUpdateCheck`, so every command removes a stale backup, the update command
  included; its error is dropped. Off Windows the hook removes nothing.

## Conventions

- Reach the outside world only through a `Config` field, never through `http.DefaultClient`,
  `time.Now`, `runtime.GOOS`, `os.Getenv` and the like called directly. A new seam is a new `Config`
  field with a documented zero-value default, resolved in `resolve`; there are four exceptions. One
  is `Install`'s unexported `fsSteps` (see the Install contract), which only in-package tests set.
  The second is the exec seam `fsSteps.run`, which `CheckStaged` runs the staged binary through and
  only in-package tests stub. The third is the `CheckWait` bound in `Finish`, a `time.NewTimer`: it
  is wall-clock time, while `Now` stamps state only. The fourth is the `runtime.GOOS` the
  `UpdateCheckHooks` pre-run hook passes to `removeStaleBackupOnHost`: it is the host's, which
  decides whether a backup can be removed while open, while `Config.GOOS` is the target platform's.
- A test that executes a staged file (any real `Apply` or `CheckStaged`, `updatecmd` tests included)
  does not call `t.Parallel()`: a process forked by a parallel test inherits the staged file's
  write descriptor until it execs, and the exec then fails with `ETXTBSY` ("text file busy").
  `updatetest.ReleaseExecutable(t, tag)` is the binary `NewRelease` packs and answers
  `version --json`: off Windows the shell script `updatetest.ReleaseBinary`, on Windows a Go program
  built with the go command on the PATH, so a test that executes it runs on every OS. A test
  comparing installed bytes compares them with `ReleaseExecutable`, never `ReleaseBinary`.
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- Commands are framework-neutral `spec.Command`s (ADR-0005). `depguard` forbids importing
  `go.vipengele.org/cli/core` and cobra anywhere in the module: update depends on no
  command-line framework. A depguard `pkg` entry matches by prefix, so a `/**` suffix matches
  nothing. The tool joins the command in its main by passing each command
  `updatecmd.Commands(cfg)` returns to `app.Add` (ADR-0009).
- `cli/spec`, `cli/state`, `commons` and `filelock` are siblings reached by directory `replace`s in
  `go.mod` (`../cli/spec`, `../cli/state`, `../commons`, `../filelock`); see the repo-root rule on
  sibling replaces.

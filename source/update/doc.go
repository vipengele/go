// Package update checks for and applies updates to a command-line tool.
//
// A tool describes itself with a [Config]: the [spec.Identity] it is declared with, the names of
// its binary and release assets, the directory its state lives in, and the [Source] that reports
// its releases. Every way the library reaches the outside world — HTTP, the clock, the target
// platform, the running executable, the environment and the user's home directory — is a
// field of Config, so a test substitutes any of them. A field left at its zero value takes its
// production default; a nil Source is the [GitHubSource] for the Identity's Owner and Repo.
// [Config.Resolved] validates a Config and returns that fully defaulted copy, so a second reader of
// the defaults, such as package [go.vipengele.org/update/installscript], which renders the tool's
// sh and PowerShell install scripts, never derives them itself.
//
// The default [GitHubSource] authenticates its REST API requests with a token: Config.Token, which
// when nil reads GH_TOKEN and then GITHUB_TOKEN through Getenv, and which opts out by returning "".
// The token goes to the API host alone: a redirect to another host fails with a
// [*CrossHostRedirectError], and the web redirect probe and every asset download are made without
// it. A 403 or 429 that reports an exhausted rate limit is a [*RateLimitError], naming when it lifts,
// and a 401 to an authenticated request is a [*TokenRejectedError]; both unwrap to the
// [*StatusError]. The source keeps each API response's ETag and body in the file "github-cache.json"
// in the StateDir and revalidates with If-None-Match, so an unchanged answer is a 304; GitHub exempts
// a 304 from the rate limit only for an authenticated request.
//
// [Config.Select] names the archive and checksums assets of a [Release] for the Config's platform.
// Their names are the AssetTemplate and ChecksumsAsset templates rendered with the placeholders of
// package asset and the AssetExt. A release that lists its assets must hold both, or Select fails
// with an [*AssetNotFoundError]. A release that does not list them (nil Assets) is located through
// the Source's [AssetLocator], which gives each asset's URL and a Size of 0, meaning unknown: a
// missing asset then surfaces at download, as an [*AssetNotFoundError] too.
//
// [Config.Download] fetches the archive a [Selection] names to a temporary file in TempDir and
// returns it as an [*Archive] only once its SHA-256 digest matches the one the release's checksums
// file lists for it. With TrustedKeys, the checksums file is trusted only once its minisign
// signature, the checksums asset's name followed by ".minisig", verifies with the trusted key its
// key id names; that check precedes parsing the checksums file and requesting the archive, so a
// [*SignatureMalformedError], an [*UnknownKeyError], a [*SignatureMismatchError] or a missing
// signature refuses the release with no archive fetched. The signed checksums file vouches for the
// archive through its digest. Under AllowUnsigned no signature is fetched and the digest check
// proves integrity only. The download is bounded: the archive may carry at most MaxDownloadBytes,
// the checksums file at most 1 MiB, the signature at most 4 KiB, and each request has a
// DownloadTimeout deadline. The temporary file is removed on every failure and by [Archive.Close].
// [WithProgress] reports the archive's progress. HTTPS is not enforced.
//
// [Config.Extract] turns the verified [*Archive] into a [*Staged] binary: a new file, mode 0700 and
// synced, in a directory the caller supplies, so that a rename into place stays on one filesystem.
// The format comes from the Config, never from the archive's bytes: an AssetTemplate without {ext}
// names a raw binary, and otherwise the AssetExt is "tar.gz" or "tgz" for a gzip-compressed tar, or
// "zip". An empty AssetExt is [asset.DefaultExtFor] the resolved GOOS: "zip" for "windows" and
// "tar.gz" for every other GOOS. Every entry of an archive is checked before anything is written, and one unsafe entry (an
// absolute or ".." path, a link, a device) rejects the whole archive with an [*UnsafeEntryError].
// The binary is the regular file named BinaryName, with ".exe" on a Windows GOOS, at any depth. The
// bytes written are bounded by MaxExtractBytes. Extract closes the archive, removing its temporary
// file, whether it succeeds or fails. [Staged.Keep] leaves the file on disk and [Staged.Discard]
// removes it; whichever is called first decides.
//
// [Config.CheckManaged] refuses an executable that an external manager owns, which an in-place
// update would fight with. It resolves the symlinks of the path Config.Executable returns through
// EvalSymlinks and returns a [*ManagedInstallError], matched with [errors.As], that names the
// manager and says what to do instead; it returns nil when the executable is not managed. The
// built-in detectors know Nix, Homebrew (macOS and Linuxbrew) and containers, and cannot be turned
// off. A tool adds its own [Detector]s through Config.Detectors, consulted in order after the
// built-in ones; the first match wins.
//
// The check has limits. The path detectors match Unix install locations, case-sensitively and on a
// directory boundary, so a Windows executable is never refused by them. Only the default Nix and
// Homebrew prefixes are known, so an install under another prefix is missed. The container check
// is a heuristic: InContainer reports what the environment and the filesystem suggest.
// [Config.Install] replaces the executable [Config.Target] names, symbolic links resolved, with a
// [*Staged] binary that lives in Target's directory. It copies the executable's permission bits to
// the staged file, renames the executable to a backup beside it, ".old" after its name, and renames
// the staged file into place; if that second rename fails, the backup is renamed back. The two
// renames are what let Windows replace a running executable, which it can rename but not overwrite.
// Install owns the [*Staged] as Extract owns the archive. The returned [*Installed] keeps the
// backup until [Installed.Discard] removes it or [Installed.Rollback] puts it back, and whichever is
// called first decides. A lack of permission is a [*PermissionError] with a hint for the user, which
// [Config.CheckWritable] can report before anything is downloaded. A [PostUpdate] hook receives
// [Installed.Path]; Install runs none itself. [Config.RemoveStaleBackup] removes a backup left
// beside the executable.
//
// [Config.Apply] is the whole update of a [Selection] in one call: CheckManaged and CheckWritable,
// both before any request, then CheckDowngrade, Download, Extract into Target's directory and
// Install, and last records the installed version in the State, stopping at the first error. It
// returns the [*Installed] with the backup still on disk; the caller runs its
// PostUpdate hooks, whose failure is reported and does not undo the update, and then settles it
// with Discard or Rollback.
//
// # Channels and downgrades
//
// Releases come in channels that accumulate: [Stable], [Beta], then [Edge], where a follower of a
// channel receives every release whose own channel is at or below it. The zero Channel is Stable.
// A release's channel is the higher of its tag's and its pre-release flag's: a tag with no suffix is
// Stable, one whose first suffix identifier is beta or rc is Beta, any other suffix is Edge, and a
// release the Source marks as a pre-release is at least Beta. Releases are ordered by semver on
// every channel. [Config.Channel] selects the channel, which a tool reads from its own
// configuration through [ParseChannel], and every [Source] receives it in Latest. The
// [GitHubSource] reads the latest-release redirect and then the API for Stable, and the release
// list page by page for Beta and Edge. Following a lower channel never downgrades: a Stable tool
// running a beta stays on it until a stable release sorts above it.
//
// The State records HighestApplied, the highest version [Config.Apply] has installed, and the
// channel LatestVersion was learnt on. A version learnt on another channel is not announced and its
// attempt does not throttle the check. [Config.CheckDowngrade] refuses a tag strictly below the
// downgrade floor, the higher of HighestApplied and the running Version, with a [*DowngradeError];
// a tag equal to the floor passes. Apply runs it unless [AllowDowngrade] is given, the only way to
// lift it, and the check's [Notice] never names a release below HighestApplied.
//
// The two halves fail differently. The guard fails closed: a State that cannot be loaded refuses
// the install, since the floor is unknown. The record fails open: after Install, a failed lock,
// load or save of HighestApplied is ignored and Apply returns the [*Installed], because the new
// binary's Version floors the next run. Deleting or corrupting the state file resets the watermark
// to the running Version.
//
// Whether a background update check may run is decided from the Config and the stored [State]. The
// gates are tried in order and the first that fails decides: the running Version is not a release
// version, the CI environment variable is set, stderr is not a terminal (IsTerminal), DisableCheck
// is set, the user set the opt-out variable of the Identity, or the last attempt is less than
// CheckInterval old. A check attempt is recorded in State, as LastAttempt, LatestChannel and, when it
// succeeded, LatestVersion, through Config.StateStore, which keeps the State between runs; a nil StateStore is
// a file in the state directory. A check stopped because the command ended is not an attempt: it
// neither throttles the next run nor erases the LatestVersion already known.
//
// # Background check
//
// [Config.Start] runs the check in the background and returns at once. It returns a nil *Check
// when the check is ineligible, and a nil *Check is valid: its Notice reports nothing and its
// Finish does nothing. [Check.Notice] never blocks; it names a release newer than the running one,
// from this run's result once the check has finished and otherwise from the LatestVersion the
// State already held. [Check.Finish] waits at most CheckWait, 250 milliseconds by default and at
// most 2 seconds, then cancels the check and joins its goroutine, so a check never delays the
// command's exit by more. A check cancelled that way is not an attempt. The bound holds while the
// Locker, the Source and the StateStore honour their context. The check runs under the Config's
// Locker, so two processes never query together; a nil Locker locks the file "update.lock" in the
// StateDir.
//
// [Config.UpdateCheckHooks] wires this around a command without importing a framework. A tool
// passes its pre-run hook to core.WithPreRun and its post-run hook to core.WithPostRun: the first
// starts the check, the second finishes it and writes the [Notice] as one line to the Env's
// Stderr, whether the command succeeded or failed. The terminal gate is decided once, when the
// check starts, so a run whose stderr is not a terminal starts no check and writes nothing. The
// hooks write nothing else and the post-run hook always returns nil. The tool's own update command
// must carry the [spec.SkipUpdateCheck] annotation, or the pre-run hook starts a check in the very
// command that installs the update. On Windows the pre-run hook also removes a stale backup, on
// every command, as described below.
//
// [Config.CheckRelease] refuses a development build: it returns a [*DevBuildError], matched with
// [errors.As], when the Config's Version is not a release version, so an update command can stop
// before it downloads anything.
//
// # Windows
//
// Windows lets a running executable be renamed but not removed, so [Installed.Discard] cannot
// remove the backup of the process that ran the update and returns a [*BackupLockedError], which
// the caller may ignore. [Config.RemoveStaleBackup] removes that backup once the process has exited,
// and the pre-run hook of [Config.UpdateCheckHooks] calls it on every command, those carrying
// [spec.SkipUpdateCheck] included, when the host is Windows, dropping its error. While an update is
// unsettled its backup is the updating process's running file there, so the hook never takes it
// from [Installed.Rollback]. On any other host the hook removes no backup: an open file can be
// removed there, and Discard always removes the backup itself. [Config.Start] does no executable
// I/O on any host.
//
// Install has one code path for every operating system. The package's tests run on Windows on a
// gated CI job, one of them replacing the file of a running process in a directory whose path
// holds a space.
//
// The Windows support has limits:
//
//   - An antivirus scanner or a search indexer that holds a file open can fail a rename. A failed
//     first rename leaves the executable untouched; a failed second rename renames the backup
//     back, and the user runs the update again. A lock that keeps the backup from being removed
//     leaves <exe>.old behind until the next run of the tool removes it, through the check hook or
//     a call of RemoveStaleBackup. Install does not retry.
//   - Paths holding spaces are supported.
//   - The sh install script refuses a zip asset, the Windows default; a Windows user installs with
//     the PowerShell script of [go.vipengele.org/update/installscript.RenderPowerShell].
//   - The PowerShell script needs Windows PowerShell 5.1 or later, and a tar.gz or tgz asset needs
//     the tar.exe of Windows 10 and later.
//
// The package depends on no command-line framework.
package update

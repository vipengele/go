// Package update checks for and applies updates to a command-line tool.
//
// A tool describes itself with a [Config]: the [spec.Identity] it is declared with, the names of
// its binary and release assets, the directory its state lives in, and the [Source] that reports
// its releases. Every way the library reaches the outside world — HTTP, the clock, the target
// platform, the running executable, the environment and the user's home directory — is a
// field of Config, so a test substitutes any of them. A field left at its zero value takes its
// production default; a nil Source is the [GitHubSource] for the Identity's Owner and Repo.
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
// file lists for it. The download is bounded: the archive may carry at most MaxDownloadBytes, the
// checksums file at most 1 MiB, and each request has a DownloadTimeout deadline. The temporary file
// is removed on every failure and by [Archive.Close]. [WithProgress] reports the archive's progress.
// HTTPS is not enforced; the digest check is the integrity guarantee.
//
// [Config.Extract] turns the verified [*Archive] into a [*Staged] binary: a new file, mode 0700 and
// synced, in a directory the caller supplies, so that a rename into place stays on one filesystem.
// The format comes from the Config, never from the archive's bytes: an AssetTemplate without {ext}
// names a raw binary, and otherwise the AssetExt is "tar.gz" or "tgz" for a gzip-compressed tar, or
// "zip". Every entry of an archive is checked before anything is written, and one unsafe entry (an
// absolute or ".." path, a link, a device) rejects the whole archive with an [*UnsafeEntryError].
// The binary is the regular file named BinaryName, with ".exe" on a Windows GOOS, at any depth. The
// bytes written are bounded by MaxExtractBytes. Extract closes the archive, removing its temporary
// file, whether it succeeds or fails. [Staged.Keep] leaves the file on disk and [Staged.Discard]
// removes it; whichever is called first decides.
//
// [Config.Install] replaces the executable [Config.Target] names, symbolic links resolved, with a
// [*Staged] binary that lives in Target's directory. It copies the executable's permission bits to
// the staged file, renames the executable to a backup beside it, ".old" after its name, and renames
// the staged file into place; if that second rename fails, the backup is renamed back. The two
// renames are what let Windows replace a running executable, which it can rename but not overwrite.
// Install owns the [*Staged] as Extract owns the archive. The returned [*Installed] keeps the
// backup until [Installed.Discard] removes it or [Installed.Rollback] puts it back, and whichever is
// called first decides. A lack of permission is a [*PermissionError] with a hint for the user, which
// [Config.CheckWritable] can report before anything is downloaded. A [PostUpdate] hook receives
// [Installed.Path]; Install runs none itself.
//
// The package depends on no command-line framework.
package update

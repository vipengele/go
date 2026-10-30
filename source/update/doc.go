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
//
// The package depends on no command-line framework.
package update

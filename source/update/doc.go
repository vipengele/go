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
// The package depends on no command-line framework.
package update

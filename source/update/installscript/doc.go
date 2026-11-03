// Package installscript renders the POSIX-sh install script of a command-line tool from its
// [go.vipengele.org/update.Config].
//
// [Render] returns a script that downloads the release asset the Config's AssetTemplate names for
// the host's platform, verifies it against the release's checksums file before anything else reads
// it, places the binary in the install directory with one rename, and installs the shell
// completion where [go.vipengele.org/cli/spec/completioninstall.Snippet] selects. A template
// holding {ext} names a tar.gz or tgz archive, which the script extracts; a template without {ext}
// names a raw binary. A zip asset has no script: Render fails with [ErrZipAsset].
//
// {name} and {ext} are fixed when the script is rendered; {os}, {arch}, {tag} and {version} are
// expanded when it runs. The script reads four environment variables, each named by
// [go.vipengele.org/cli/spec.Identity.EnvVar]: <PREFIX>_VERSION picks a release, as v1.2.3 or
// 1.2.3, in place of the latest; <PREFIX>_INSTALL_DIR replaces $HOME/.local/bin;
// <PREFIX>_BASE_URL replaces https://github.com; and <PREFIX>_NO_COMPLETION set to 1 skips the
// completion. A completion that cannot be installed is a warning, never a failure.
package installscript

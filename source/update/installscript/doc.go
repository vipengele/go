// Package installscript renders the POSIX-sh install script of a command-line tool from its
// [go.vipengele.org/update.Config].
//
// [Render] returns a script that downloads the release asset the Config's AssetTemplate names for
// the host's platform, verifies it against the release's checksums file before anything else reads
// it, places the binary in the install directory with one rename, and installs the shell
// completion where [go.vipengele.org/cli/spec/completioninstall.Snippet] selects. A template
// holding {ext} names a tar.gz or tgz archive, which the script extracts; a template without {ext}
// names a raw binary. A zip asset has no script: Render fails with [ErrZipAsset]. Render validates
// the Config as [go.vipengele.org/update.Config.Validate] does, so a Config that states no
// signature policy, or two, has no script either.
//
// {name} and {ext} are fixed when the script is rendered; {os}, {arch}, {tag} and {version} are
// expanded when it runs. The script reads four environment variables, each named by
// [go.vipengele.org/cli/spec.Identity.EnvVar]: <PREFIX>_VERSION picks a release, as v1.2.3 or
// 1.2.3, in place of the latest; <PREFIX>_INSTALL_DIR replaces $HOME/.local/bin;
// <PREFIX>_BASE_URL replaces https://github.com; and <PREFIX>_NO_COMPLETION set to 1 skips the
// completion. A completion that cannot be installed is a warning, never a failure. A script
// rendered with TrustedKeys also reads <PREFIX>_ALLOW_UNSIGNED, described below.
//
// The script's signature policy is the Config's (ADR-0025). Under AllowUnsigned it checks the
// digest alone and fetches no signature. With TrustedKeys it fetches <checksums>.minisig after the
// checksums file and fails when the release has none; each key is embedded as its 56-character key
// line, never as the TrustedKeys entry, so no comment line or whitespace reaches the shell. When
// minisign is on PATH, the script runs `minisign -V -P <key> -m <checksums> -x <signature>` once
// per key and fails unless one verifies. When it is not, the script warns on stderr that the
// signature cannot be checked, then installs on the checksum alone only when
// <PREFIX>_ALLOW_UNSIGNED is 1, or when a /dev/tty it can open is answered "y" or "yes", in any
// case; any other answer and end of input refuse. With neither the variable nor a terminal it
// fails, naming both remedies: install minisign, or set <PREFIX>_ALLOW_UNSIGNED=1. Every refusal
// comes before anything is installed. The variable has no effect when minisign is on PATH.
//
// Without <PREFIX>_VERSION the latest release is the last path segment of the Location header of
// the unfollowed redirect from <base>/<owner>/<repo>/releases/latest; there is no API fallback.
// The script is Unix only (Linux and macOS, amd64 and arm64) and reads releases from a GitHub-style
// server alone: Config.Source, HTTPClient, the GOOS and GOARCH overrides and the size limits belong
// to the Go updater and are ignored.
package installscript

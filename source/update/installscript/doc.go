// Package installscript renders the install scripts of a command-line tool from its
// [go.vipengele.org/update.Config]: a POSIX-sh script for Linux and macOS, and a PowerShell script
// for Windows.
//
// # The sh script
//
// [Render] returns a script that downloads the release asset the Config's AssetTemplate names for
// the host's platform, verifies it against the release's checksums file before anything else reads
// it, places the binary in the install directory with one rename, and installs the shell
// completion where [go.vipengele.org/cli/spec/completioninstall.Snippet] selects. A template
// holding {ext} names a tar.gz or tgz archive, which the script extracts; a template without {ext}
// names a raw binary. A zip asset has no sh script: Render fails with [ErrZipAsset]. Render validates
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
// The script's signature policy is the Config's (ADR-0026). Under AllowUnsigned it checks the
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
// server alone: Config.Source, HTTPClient, the GOARCH override and the size limits belong to the Go
// updater and are ignored. Render resolves the Config with GOOS "linux" when it names none, so the
// defaults, and the script, are the same on every host; a Config naming GOOS "windows" resolves
// AssetExt to "zip" and has no sh script.
//
// # The PowerShell script
//
// [RenderPowerShell] returns the same install for Windows, amd64 and arm64: the same releases,
// checksums parsing, signature policy, tokens and environment variables, and the same order, with
// every refusal before anything is installed. It runs on Windows PowerShell 5.1 and PowerShell 7,
// saved to a file or as `irm <url> | iex`; it is one script block run with `&`, so it sets nothing
// in the caller's session but $env:Path, and it reports a failure as a terminating error with the
// message "<name>: error: ..." rather than with `exit`, which would close the caller's window.
// RenderPowerShell resolves the Config with GOOS "windows" when it names none, so AssetExt
// defaults to "zip".
//
// {os} is windows and {arch} comes from PROCESSOR_ARCHITEW6432, else PROCESSOR_ARCHITECTURE:
// AMD64 is amd64, ARM64 is arm64 and anything else fails. The binary is <BinaryName>.exe, found
// at any depth of a zip archive, which Expand-Archive extracts, or of a tar.gz or tgz archive,
// which needs the tar.exe of Windows 10 and later in the system directory; a template without
// {ext} names the binary itself. Downloads go through System.Net.Http.HttpClient, with TLS 1.2
// added to the session's protocols for .NET Framework, and digests through Get-FileHash.
//
// The script reads the sh script's variables, with %LOCALAPPDATA%\Programs\<name> as the default
// install directory, and <PREFIX>_NO_MODIFY_PATH. It installs through a staged file in the install
// directory: an existing binary, which Windows lets a running process keep open, is moved aside to
// <name>.exe.old first, and that file is removed once nothing runs it. When the install directory
// is on neither the session's PATH nor the user PATH in HKEY_CURRENT_USER\Environment, the script
// appends it to the registry value, read unexpanded and written back as REG_EXPAND_SZ, or as
// REG_SZ when it was one, broadcasts WM_SETTINGCHANGE and adds the directory to the session's
// PATH; <PREFIX>_NO_MODIFY_PATH set to 1 replaces all of that with a warning. The PowerShell
// completion is installed by [go.vipengele.org/cli/spec/completioninstall.PowerShellSnippet]
// unless <PREFIX>_NO_COMPLETION is 1. Without minisign.exe on PATH, a script rendered with
// TrustedKeys prompts with Read-Host only when the session is interactive: the process runs in a
// user's session, standard input is not redirected and PowerShell was not started with
// -NonInteractive. Otherwise it needs <PREFIX>_ALLOW_UNSIGNED=1, as the sh script does without a
// terminal.
package installscript

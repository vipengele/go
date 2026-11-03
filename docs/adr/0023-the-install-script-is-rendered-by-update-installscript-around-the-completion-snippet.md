# The install script is rendered by `update/installscript` around the completion snippet

Status: accepted. It follows [0003-the-library-is-three-projects-cli-update-and-daemon.md](0003-the-library-is-three-projects-cli-update-and-daemon.md), which
assigns install-script templates to `update`, and reconciles it with
[0021-the-completion-installer-is-cli-spec-completioninstall-with-an-update-post-hook.md](0021-the-completion-installer-is-cli-spec-completioninstall-with-an-update-post-hook.md), which
keeps the completion path table in `cli/spec` and leaves the full `install.sh` to each tool. The
verification rule is that of [0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md](0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md).

A tool installs itself on a Unix machine with one POSIX-sh script. The public package
`go.vipengele.org/update/installscript` renders that script from the tool's `update.Config`:
`Render(cfg update.Config) ([]byte, error)`. The script downloads the release asset the `Config`
describes, verifies it against the release's checksums file, places the binary and installs the
shell completion.

## Decisions

- **Placement.** `installscript` is a new public package of the `update` project, module
  `go.vipengele.org/update/installscript`. Its template inserts the output of
  `completioninstall.Snippet(BinaryName)` verbatim and never copies the completion path table into
  `update`. ADR-0021 put the table and its sh form in `cli/spec/completioninstall` and called a full
  `install.sh` out of scope, each tool embedding the snippet in its own script; ADR-0003 puts install
  templates in `update`. The two agree only when `update`'s template embeds the rendered snippet.
  The snippet's own test proves that the path it selects equals the Go `Target` for every shell, so
  the script inherits that guarantee.
- **Defaults exist once.** `update` exports `func (c Config) Resolved() (Config, error)`, a wrapper
  of the unexported `resolve` that validates the `Config` and fills every default. `Render` calls it
  and `installscript` never derives a default itself: applying the `asset.Default*` constants a
  second time would recreate the drift between the updater and the installer.
- **Input.** `Render` takes the `update.Config` and reads the asset template from the resolved
  value.
  - A template holding `{ext}` with `AssetExt` `tar.gz` or `tgz` renders the archive path: download,
    verify, extract.
  - A template with no `{ext}` renders the raw-binary path: download, verify, `chmod`, `mv`, no
    `tar`. `Validate` ignores `AssetExt` then.
  - A template holding `{ext}` with `AssetExt` `zip` is a typed error. The script has no `unzip`
    path.
  - `{name}` and `{ext}` are baked in at render time. `{os}`, `{arch}`, `{version}` and `{tag}` are
    shell variables expanded when the script runs. `{version}` is the tag without its leading `v`.
- **Overrides.** Each variable name is built with `Identity.EnvVar(suffix)`, so no prefix is spelled
  out in the package.
  - `<P>_VERSION` accepts `v1.2.3` or `1.2.3` and is normalised to the tag `v1.2.3`. When unset the
    latest release is the last path segment of the `Location` header of the unfollowed `302` from
    `<base>/<owner>/<repo>/releases/latest`; there is no API fallback. A tag with any character
    outside `[A-Za-z0-9._+-]` is rejected and is not URL-decoded.
  - `<P>_INSTALL_DIR` defaults to `$HOME/.local/bin`.
  - `<P>_NO_COMPLETION` disables the completion step when its value is `1` and no other value does
    (`completioninstall.NoCompletionEnvSuffix`).
  - `<P>_BASE_URL` defaults to `https://github.com`. A download is
    `<base>/<owner>/<repo>/releases/download/<tag>/<asset>`.
- **Behaviour.** The script runs under `set -eu`.
  - It uses `curl`, else `wget`, else fails. It uses `sha256sum`, else `shasum -a 256`, else fails.
    Verification is never skipped, and the asset is checked against its line in the checksums file
    before anything else touches it. HTTPS is not enforced: the digest is the guarantee (ADR-0020).
  - `uname -s` maps to `linux` or `darwin`; `uname -m` maps `x86_64` to `amd64` and `aarch64` or
    `arm64` to `arm64`. Anything else fails with a message naming the value.
  - Download and extraction happen inside `mktemp -d`. Extraction must find exactly one regular file
    named `BinaryName` (`find -type f`).
  - The script runs `mkdir -p` on the install directory, stages a temporary file inside it, sets
    mode 755 and `mv`s it over the target, so the replacement is one rename on one filesystem. A
    `cleanup` function, run by a trap on `EXIT` and by traps on `INT` and `TERM`, removes the
    temporary directory and any pending stage file. The signal traps exit explicitly (130 and 143)
    after `cleanup`, because a signal handler that returns resumes the script.
  - An HTTP 404 on the asset reports a missing asset naming the asset and the tag.
  - The script warns when no `$PATH` entry equals the install directory, compared literally with a
    trailing `/` trimmed from both sides.
- **Completion.** Once the binary is in place the shell is `basename "$SHELL"`. The snippet's
  `completion_target` is called as `if completion_target "$sh"; then`, never bare: it returns 1 or 2
  by design and `set -e` would abort the script. The script runs `mkdir -p` on the target's
  directory, runs `"$INSTALL_DIR/<name>" completion <sh>` into a stage file in that directory, `mv`s
  it over the target and prints `COMPLETION_NOTE` when the snippet sets it. Every completion failure
  is a warning and the exit status stays 0: no home directory, an unsupported shell, and a tool built
  without `core.WithCompletion()` whose `completion` command exits non-zero. No rc file is edited.
- **Limits.** The script ignores `Config.Source`, `HTTPClient`, the `GOOS` and `GOARCH` overrides and
  the size limits, which belong to the Go updater. It is Unix only and handles `tar.gz`, `tgz` and
  raw binaries only.

## Considered options

- **Copying the path table into `update`.** Two hand-maintained copies drift without a test able to
  see both. ADR-0021 rejected it for the same reason.
- **The full script in `cli/spec/completioninstall`.** It would put the release contract, the asset
  naming, the checksums and the download layout, in `cli/spec`, which knows nothing of releases.
- **`spec.Identity` as the only input.** The identity cannot follow a custom `AssetTemplate`, so a
  tool with its own asset naming would get a script that downloads a file that does not exist.
- **Re-deriving the defaults in `installscript`.** It repeats the `asset.Default*` constants in a
  second package, which is the drift `Resolved` removes.
- **Zip support.** It needs `unzip`, which is absent from a minimal system more often than `tar`,
  and the two archive paths would each need the single-file extraction rule.
- **An API fallback for the latest version.** It needs JSON parsing in sh, and the API is
  rate-limited where the `releases/latest` redirect is not.

## Consequences

- **`update` has a second reader of the completion snippet.** A change to the snippet changes every
  script rendered after it; a script already distributed keeps the snippet it was rendered with.
- **The `Config` contract is public.** `Config.Resolved` is exported so that the installer and the
  updater read one set of defaults.
- **A tool with `AssetExt` `zip` has no generated script.** It writes its own, or releases a
  `tar.gz`.
- **A tool built without `core.WithCompletion()` installs with a warning.** The binary is in place
  and the exit status is 0.

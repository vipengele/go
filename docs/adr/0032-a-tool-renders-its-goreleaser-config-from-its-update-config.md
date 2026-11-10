# A tool renders its GoReleaser config from its update config

Status: accepted. It follows the install-script mechanism of ADR-0023, signs as the minisign
signature of ADR-0024 and the unsigned opt-out of ADR-0026 decide, and keeps to ADR-0008, which
takes the identity of `update` from `cli/spec` and keeps it from importing `cli/core`. The rendered
`formats` and `binary` are the archive format and binary name that the extraction of ADR-0021
derives from the same `Config`, and the format of each platform follows the per-GOOS default
extension of ADR-0031.

`update` selects, verifies and extracts the assets of a release by the names its `Config` gives
them. A GoReleaser config written by hand drifts from those names, and a tool then publishes assets
its own updater does not find. `go.vipengele.org/update/goreleaser` renders the config from the
`Config`: `Render(cfg update.Config, opts Options) ([]byte, error)`.

## Decisions

- **Link variables.** The build sets three variables with `-ldflags "-s -w -X <version>={{ .Tag }}
  -X <commit>={{ .FullCommit }} -X <date>={{ .Date }}"`. The variables default to
  `go.vipengele.org/cli/core/buildinfo.version`, `.commit` and `.date`, exported as the constants
  `DefaultVersionVar`, `DefaultCommitVar` and `DefaultDateVar` and as `DefaultLinkVars()`, and
  `Options.LinkVars` overrides each. They are not a package-level variable: `gochecknoglobals` is
  enabled.
- **The version is `.Tag`.** GoReleaser strips the `v` from `.Version`; a binary linked with
  `1.2.3` is not a release version to `cli/spec/version.IsRelease`, reads as a development build,
  and its updater never checks for or applies an update. `.Tag` keeps the `v`.
- **Signing is in the template.** Unless the `Config` sets `AllowUnsigned`, the config carries the
  `signs:` block of ADR-0024: `artifacts: checksum`, `cmd: minisign`, `args: -S -s <key> -m
  ${artifact} -x ${signature}`, `signature: "${artifact}.minisig"`. `Options.SigningKeyPath` is the
  key, `minisign.key` by default. Under `AllowUnsigned` there is no block and `update` fetches no
  signature (ADR-0026). The release workflow that writes the key file and runs GoReleaser is outside
  the package.
- **Mechanism of ADR-0023.** The config is the embedded `goreleaser.yaml.in` with `@@TOKEN@@`
  tokens, filled by one `strings.NewReplacer` pass, so a value holding a token stays literal. Every
  inserted value is a double-quoted YAML scalar from one escaping helper. A value GoReleaser
  expands as a template, the binary name, the link variables and the key path, has each `{` and `$`
  written as a template action that yields it. Production code uses no YAML library.
- **Inputs come from `Config.Resolved`.** Name, binary name, asset template, checksums asset and
  signature policy are never derived again, so `Render` inherits the requirements of `Resolved`
  (ADR-0023). `Main`, `Dir`, `Platforms`, `LinkVars` and `SigningKeyPath` come from `Options`.
- **The extension is decided per platform.** `Resolved` fills an empty `AssetExt` for the one GOOS
  the `Config` names, the host's by default, but a release serves every platform. The extension of
  a platform is the caller's `AssetExt` when set, else `asset.DefaultExtFor` of that platform's
  GOOS (ADR-0031): `zip` for windows, `tar.gz` for every other GOOS. The archive's `formats` is the
  format of the first GOOS of `Platforms`; a `format_overrides` entry (`goos`, `formats`) gives each
  other GOOS whose format differs its own, and the key is left out when every platform shares one.
  A raw-binary template is `binary` on every platform. The rendered config is therefore the same on
  every host and for every GOOS the `Config` names.
- **Scope is the contract and nothing else.** The config holds `version: 2`, `project_name`, one
  build (`binary`, `main`, `dir` when set, `CGO_ENABLED=0`, `ldflags`, `goos`, `goarch`, `ignore`),
  one archive (`name_template` from `asset.GoReleaserArchive`, `formats` and, when a GOOS differs,
  `format_overrides`, as above), `checksum` (`asset.GoReleaserChecksum`, `sha256`) and the
  optional `signs`. It holds no `changelog`, `release`, `before` or `snapshot`; a tool adds those
  without changing the keys above. GoReleaser puts its default README and LICENSE files in an
  archive; that is accepted because the extractor takes the one file named `BinaryName` and
  tolerates the rest. The minimum is GoReleaser v2.6, the first with the plural `formats`, in the archive and in
  `format_overrides`.
- **Refusals.** `Render` fails, each wrapping a sentinel, on: an empty `Main` (`ErrNoMain`); an
  empty or repeated platform (`ErrEmptyPlatform`, `ErrDuplicatePlatform`); a `ChecksumsAsset`
  holding `{os}` or `{arch}`, since GoReleaser writes one checksums file per release
  (`ErrPlatformChecksums`); a raw-binary `AssetTemplate` with a windows platform, since GoReleaser
  appends `.exe` to the name of a binary-format asset (`ErrRawWindowsAsset`); an `AssetTemplate`
  that renders the same name for two configured platforms, since GoReleaser publishes both archives
  under that one name (`ErrSharedAssetName`); a link variable holding whitespace, a quote or `=`
  (`ErrInvalidLinkVar`); and a value that is not valid UTF-8 (`ErrNotUTF8`). An error of `Resolved`, `asset.GoReleaserArchive` or `asset.GoReleaserChecksum`
  passes through.

## Considered options

- **`{{ .Version }}` for the version.** It is the common form in GoReleaser configs and it drops
  the `v`, which leaves the tool unable to update itself (above).
- **Marshalling a struct with `yaml/v3`.** It would couple the production package to a YAML library
  and to GoReleaser's schema as Go types, which the package would have to track. The tests use
  `yaml/v3` to parse the rendered config; production code does not.
- **A package-level default for the link variables.** `gochecknoglobals` refuses it; constants and
  `DefaultLinkVars()` give the same defaults.
- **One `formats` from the resolved `AssetExt`.** It is the extension for the GOOS the `Config`
  resolves for, the host's by default: rendered on Linux with no `AssetExt`, a windows archive
  would be published as `tar.gz` while `Select` on windows looks for `zip`, and the same `Config`
  would render differently on a Windows host.
- **A fixed `formats` of the non-windows default.** It would need a GOOS standing for "not
  windows" beside `asset.DefaultExtFor`, a second statement of which GOOS has which default; the
  first platform's format needs none, and either choice publishes the same names.
- **Leaving signing to the tool.** The signature name `update` fetches is `<checksums>.minisig`; a
  hand-written `signs` block is one more place for the two to differ.

## Consequences

- **A rename in `buildinfo` is not caught at compile time.** `update` may not import `cli/core`
  (ADR-0008), so the default paths are strings. A test cannot see `buildinfo` either; the cost is
  accepted and the defaults are overridable through `Options.LinkVars`.
- **The published names are the ones `Config.Select` looks for.** The tests round-trip the parsed
  config through `Config.Select`.
- **A tool with a raw-binary asset cannot target windows through this package.** It uses an
  archive, or writes its own config.
- **A tool needs GoReleaser v2.6 or later.**

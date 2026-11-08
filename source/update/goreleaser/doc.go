// Package goreleaser renders the GoReleaser config that publishes a command-line tool's releases
// in the form its [go.vipengele.org/update.Config] reads them.
//
// [Render] returns a GoReleaser v2 config, for GoReleaser v2.6 or later (the archive key formats,
// plural, is no older), holding only what update depends on:
//
//   - version 2 and project_name, the Identity's Name.
//   - One build: binary, the resolved BinaryName, which [go.vipengele.org/update.Config.Extract]
//     matches by base name, with ".exe" on windows as GoReleaser names it; main and, when set, dir
//     from [Options]; CGO_ENABLED=0; the ldflags "-s -w" and a -X of each [LinkVars] variable, set to
//     {{ .Tag }}, {{ .FullCommit }} and {{ .Date }}; and goos, goarch and ignore lists that build
//     exactly Options.Platforms. The version is the tag, "v" included, because {{ .Version }} drops
//     the "v" and a version without it is not a release version to
//     go.vipengele.org/cli/spec/version.IsRelease, so the tool would never check for an update.
//   - One archive: name_template from [go.vipengele.org/update/asset.GoReleaserArchive] over the
//     AssetTemplate, and formats the AssetExt ("tar.gz", "tgz" or "zip"), or "binary" when the
//     AssetTemplate holds no {ext}.
//   - checksum: name_template from [go.vipengele.org/update/asset.GoReleaserChecksum] over the
//     ChecksumsAsset, and algorithm sha256.
//   - Unless the Config sets AllowUnsigned, the signs block of ADR-0024: minisign signs the checksums
//     file with the secret key at Options.SigningKeyPath, as "<checksums>.minisig", the signature
//     update fetches. Under AllowUnsigned there is no signs block, and update fetches no signature.
//
// It emits no before hooks, changelog, release, snapshot or any other block: a tool adds those to
// the rendered file, or merges them in, and changes none of the keys above, or the published names
// and the ones update selects differ.
//
// Every value of the Config comes from [go.vipengele.org/update.Config.Resolved], never derived
// again here, so Render inherits its requirements: the Config is valid, it states exactly one
// signature policy, TrustedKeys or AllowUnsigned, and its StateDir resolves, which for an empty
// StateDir needs the state directory of the Identity to be resolvable. Main, Dir, Platforms,
// LinkVars and the signing key path come from Options; Platforms defaults to linux and darwin on
// amd64 and arm64, LinkVars to [DefaultLinkVars], the variables of
// go.vipengele.org/cli/core/buildinfo, and the key path to [DefaultSigningKeyPath].
//
// Render refuses a Config and Options GoReleaser would publish under names update does not read: a
// ChecksumsAsset holding {os} or {arch}, since GoReleaser writes one checksums file per release, and
// a raw-binary AssetTemplate built for windows, since GoReleaser appends ".exe" to that asset's
// name, and an AssetTemplate that names two of the platforms alike, since GoReleaser publishes
// both archives under one name. It also refuses an empty Main, an empty or repeated platform, a link variable the -X flag
// cannot carry, and a value that is not valid UTF-8.
package goreleaser

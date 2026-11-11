// Package releasetest verifies a release GoReleaser built from a config that
// [go.vipengele.org/update/goreleaser.Render] rendered, before anything is published: it checks
// that the files in GoReleaser's dist directory are the ones a tool's [update.Config] downloads,
// verifies and extracts. It is support for tests and CI, not part of update's API.
//
// [Verify] checks, for a dist directory, a tag and the Config the release was rendered from:
//
//   - that dist/metadata.json names the tag;
//   - the platforms the release built, read from the "Binary" artifacts dist/artifacts.json lists,
//     and for each that [update.Config.Select] picks an archive, a checksums file and a signature
//     present in the dist directory, with the tag as the release's tag, so an AssetTemplate that
//     holds {tag} or {version} names an archive carrying the tag;
//   - the minisign signature of each checksums file Select picks, against the public key file it is
//     given, through package go.vipengele.org/update/internal/minisign;
//   - every SHA-256 digest each checksums file lists, against the file of that name in the dist
//     directory, and that each selected archive is listed.
//
// The platforms come from artifacts.json, not from the names in the checksums file: a name does not
// say which platform it was built for without parsing it against the AssetTemplate, and a platform
// whose archive GoReleaser named differently from the one Select renders would leave no name to
// find. artifacts.json lists what was built whatever the archives are called, so such an archive is
// reported missing.
//
// A Config under AllowUnsigned is refused: a release built from it carries no signature, and
// Verify checks a signed release.
package releasetest

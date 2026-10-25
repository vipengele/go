// Package asset names the files a release publishes for a tool, from a template.
//
// A template is literal text with placeholders in braces:
//
//	{name}     the binary name
//	{version}  the release tag without its leading "v": "1.2.3" for "v1.2.3"
//	{tag}      the release tag as published: "v1.2.3"
//	{os}       the target operating system, such as "linux"
//	{arch}     the target architecture, such as "amd64"
//	{ext}      the archive extension, such as "tar.gz"
//
// Every "{" opens a placeholder, so a template holds no literal brace. An unknown placeholder, a
// "{" without its "}" and an empty template are errors. [Render] substitutes the placeholders in
// one left-to-right pass: a substituted value is never expanded again, so a binary name or tag
// that itself contains "{os}" appears as written. The archive template defaults to
// [DefaultTemplate] and the checksums template to [DefaultChecksumsTemplate]. The checksums
// template takes every placeholder except {ext}, which [ValidateChecksums] rejects.
//
// [GoReleaserArchive] and [GoReleaserChecksum] translate a template into a GoReleaser
// name_template that names the same files. The binary name is inlined as literal text, and
// {version}, {tag}, {os} and {arch} become {{ .Version }}, {{ .Tag }}, {{ .Os }} and
// {{ .Arch }}. GoReleaser appends the archive extension itself, from the archive's format, so
// GoReleaserArchive drops a trailing ".{ext}" and the GoReleaser format must equal the
// Config.AssetExt the template is rendered with; otherwise the published name and the one the
// library looks for differ.
//
// The two renderings accept different languages. Render is generic: {ext} is valid anywhere in a
// template, and in a checksums template too. GoReleaserArchive accepts {ext} only as the trailing
// ".{ext}" and fails on it anywhere else, and GoReleaserChecksum fails on it anywhere at all,
// because GoReleaser has no field that stands for it. A template valid for Render may therefore
// have no GoReleaser equivalent.
package asset

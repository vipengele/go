package goreleaser

import "errors"

var (
	// ErrNoMain reports Options whose Main is empty: a build with no main package builds nothing.
	ErrNoMain = errors.New("options name no main package")
	// ErrEmptyPlatform reports a [Platform] with an empty GOOS or GOARCH.
	ErrEmptyPlatform = errors.New("empty platform")
	// ErrDuplicatePlatform reports a [Platform] listed twice in Options.Platforms.
	ErrDuplicatePlatform = errors.New("duplicate platform")
	// ErrPlatformChecksums reports a ChecksumsAsset holding {os} or {arch}. GoReleaser writes one
	// checksums file for every platform of a release, so its name cannot vary by platform.
	ErrPlatformChecksums = errors.New("checksums template varies by platform")
	// ErrRawWindowsAsset reports an AssetTemplate without {ext}, a raw binary, rendered for a
	// windows platform. GoReleaser appends ".exe" to the name of a binary-format asset for windows,
	// so the published asset is not the one update selects.
	ErrRawWindowsAsset = errors.New("raw binary asset for windows")
	// ErrInvalidLinkVar reports a [LinkVars] path holding whitespace, a quote or "=", which would
	// split or end the -X flag it is written into.
	ErrInvalidLinkVar = errors.New("invalid link variable")
	// ErrNotUTF8 reports a value that is not valid UTF-8, which a YAML document cannot carry.
	ErrNotUTF8 = errors.New("value is not valid UTF-8")
)

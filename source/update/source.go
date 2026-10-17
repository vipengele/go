package update

import "context"

// Source reports the releases a tool can update to. A [Config] without one uses the [GitHubSource]
// for its Identity.
type Source interface {
	// Latest returns the release the tool should consider updating to. It returns an error when
	// the source cannot be reached or holds no release; ctx bounds the whole lookup.
	Latest(ctx context.Context) (Release, error)
}

// Release is one published version of a tool.
type Release struct {
	// Tag is the release's version tag, as the source publishes it, such as "v1.2.3".
	Tag string
	// URL is the release's human-readable page.
	URL string
	// Assets are the files published with the release.
	Assets []Asset
}

// Asset is one file published with a [Release].
type Asset struct {
	// Name is the file name, which an asset is matched by.
	Name string
	// URL is where the file is downloaded from.
	URL string
	// Size is the file's length in bytes, or 0 when the source does not report it.
	Size int64
}

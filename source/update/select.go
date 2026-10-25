package update

import (
	"errors"
	"fmt"

	"go.vipengele.org/update/asset"
)

// AssetLocator is implemented by a [Source] that can say where an asset is downloaded from
// without listing the release's assets. [Config.Select] consults it for a [Release] whose Assets
// are nil.
type AssetLocator interface {
	// AssetURL returns the download address of the asset name of the release tagged tag.
	AssetURL(tag, name string) string
}

var _ AssetLocator = (*GitHubSource)(nil)

// Selection is the pair of release assets an update downloads for one platform.
type Selection struct {
	// Archive holds the binary for the platform.
	Archive Asset
	// Checksums lists the checksum of every asset of the release.
	Checksums Asset
}

// Select returns the assets of rel that the Config names for its platform. The archive's name is
// AssetTemplate rendered for the BinaryName, rel's Tag, the GOOS and GOARCH and the AssetExt, and
// the checksums asset's name is ChecksumsAsset rendered for the same values, each after the
// Config is resolved; Select fails, without looking at rel, when the Config does not resolve.
//
// When rel lists its assets, a non-nil Assets, each name must match one exactly: an empty list
// matches nothing. A missing asset is a [*AssetNotFoundError], and when both are missing the error
// joins one for each. When rel's Assets are nil, meaning not listed, Select asks the resolved
// Source for each asset's URL through [AssetLocator] and returns assets of Size 0; a Source that
// is not an AssetLocator gives an error wrapping [ErrAssetsNotListed].
func (c Config) Select(rel Release) (Selection, error) {
	r, err := c.resolve()
	if err != nil {
		return Selection{}, err
	}
	return r.selectAssets(rel)
}

// selectAssets is [Config.Select] for a Config that is already resolved. It fails on a template
// that does not render, which a resolved Config never holds.
func (r Config) selectAssets(rel Release) (Selection, error) {
	fields := asset.Fields{Name: r.BinaryName, Tag: rel.Tag, OS: r.GOOS, Arch: r.GOARCH, Ext: r.AssetExt}
	archiveName, err := asset.Render(r.AssetTemplate, fields)
	if err != nil {
		return Selection{}, fmt.Errorf("update: rendering AssetTemplate: %w", err)
	}
	checksumsName, err := asset.Render(r.ChecksumsAsset, fields)
	if err != nil {
		return Selection{}, fmt.Errorf("update: rendering ChecksumsAsset: %w", err)
	}

	if rel.Assets == nil {
		loc, ok := r.Source.(AssetLocator)
		if !ok {
			return Selection{}, fmt.Errorf("%w: release %s, source %T", ErrAssetsNotListed, rel.Tag, r.Source)
		}
		return Selection{
			Archive:   Asset{Name: archiveName, URL: loc.AssetURL(rel.Tag, archiveName)},
			Checksums: Asset{Name: checksumsName, URL: loc.AssetURL(rel.Tag, checksumsName)},
		}, nil
	}

	archive, archiveErr := findAsset(rel, archiveName)
	checksums, checksumsErr := findAsset(rel, checksumsName)
	if err := errors.Join(archiveErr, checksumsErr); err != nil {
		return Selection{}, err
	}
	return Selection{Archive: archive, Checksums: checksums}, nil
}

// findAsset returns the asset of rel named name exactly, or a [*AssetNotFoundError].
func findAsset(rel Release, name string) (Asset, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	return Asset{}, &AssetNotFoundError{Tag: rel.Tag, Name: name}
}

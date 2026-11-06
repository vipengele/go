package update

import "go.vipengele.org/cli/spec/version"

// CheckRelease refuses a development build. It returns a [*DevBuildError], matched with
// [errors.As], when the Config's Version is not a release version according to [version.IsRelease]
// (empty, "dev", a pseudo-version or one with build metadata), and nil otherwise. A build that is
// not a release has no version to compare against, so an update would replace it blindly.
func (c Config) CheckRelease() error {
	r, err := c.resolve()
	if err != nil {
		return err
	}

	if version.IsRelease(r.Version) {
		return nil
	}

	return &DevBuildError{Name: r.Name, Version: r.Version}
}

package update

import (
	"context"

	"go.vipengele.org/cli/spec/version"
)

// CheckDowngrade refuses to install tag when it is below the downgrade floor. It returns a
// [*DowngradeError], matched with [errors.As], when it is, and nil otherwise.
//
// The floor is the higher, in the order of [version.Newer], of the State's HighestApplied and the
// running Version, each ignored when it is not a release version according to [version.IsRelease].
// A tag equal to the floor or above it passes, and so does any tag when neither yields a floor, as
// for a development build with no recorded watermark. A tag that is not a release version is below
// every floor.
//
// The check fails closed: it returns c's validation error for an invalid Config, and the
// StateStore's error when the State cannot be loaded, rather than guess at the floor. A missing or
// corrupt state file loads as the zero State, so deleting or damaging it resets the watermark and
// the floor falls back to the running Version.
func (c Config) CheckDowngrade(ctx context.Context, tag string) error {
	r, err := c.resolve()
	if err != nil {
		return err
	}

	st, err := r.StateStore.Load(ctx)
	if err != nil {
		return err
	}

	floor := downgradeFloor(st.HighestApplied, r.Version)
	if version.Newer(tag, floor) {
		return &DowngradeError{Name: r.Name, Version: tag, Floor: floor}
	}

	return nil
}

// downgradeFloor returns the higher of highest and current that is a release version, or empty
// when neither is.
func downgradeFloor(highest, current string) string {
	floor := ""

	for _, v := range []string{highest, current} {
		if version.IsRelease(v) && (floor == "" || version.Newer(floor, v)) {
			floor = v
		}
	}

	return floor
}

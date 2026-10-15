package version

import (
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// IsRelease reports whether v is a release version: vMAJOR.MINOR.PATCH, optionally with a
// prerelease, with no build metadata, and not a Go pseudo-version. It is false for every other
// string, including the empty string.
func IsRelease(v string) bool {
	return semver.IsValid(v) && semver.Canonical(v) == v && !module.IsPseudoVersion(v)
}

// Newer reports whether latest is a release version that sorts above current, in semantic
// version order, where a prerelease sorts below the release it precedes.
//
// A latest that is not a release version is never newer. A current that is not a release version
// is a dev build, and a dev build is older than every release version: Newer is true for it
// whenever latest is a release version.
func Newer(current, latest string) bool {
	if !IsRelease(latest) {
		return false
	}
	if !IsRelease(current) {
		return true
	}
	return semver.Compare(latest, current) > 0
}

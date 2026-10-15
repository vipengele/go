// Package version classifies and orders the version strings a command-line tool compares when it
// updates itself: the version of the running binary and the latest version a release source
// offers.
//
// A release version is vMAJOR.MINOR.PATCH, optionally with a prerelease (v1.2.3, v1.2.3-rc.1),
// that carries no build metadata and is not a Go pseudo-version. Every other string is not a
// release version: an empty version, (devel), dev, a version carrying build metadata such as
// v1.2.3+dirty, every pseudo-version, the shorthand forms v1 and v1.2, and a version without its
// leading v. ADR-0007 (docs/adr/0007-the-cli-project-owns-the-version-json-contract.md) defines
// the rule for the running binary, whose version is a dev build when it is not a release version;
// this package applies the same rule to the latest version a release source reports.
//
// A release version here is a single version string. It is not a release in the sense of one
// project published at one version under its tag.
//
// [IsRelease] classifies one version. [Newer] reports whether the latest version is a release
// version that sorts above the current one, and treats a current version that is not a release
// version as a dev build, older than every release version.
//
// Two consequences follow from the rule:
//
//   - Newer is pure ordering. A prerelease sorts above the release before it, so
//     Newer("v1.2.3", "v1.3.0-rc.1") is true; whether a user on a stable version is offered a
//     prerelease is the caller's decision.
//   - A release source tag published without its leading v, such as 1.2.3, is not a release
//     version, so Newer reports false for it, and the caller cannot tell that apart from being up
//     to date. A release tagged that way is never offered, and nothing reports why.
package version

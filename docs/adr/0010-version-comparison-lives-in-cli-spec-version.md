# Version comparison lives in `cli/spec/version`

Status: amends ADR-0003. It departs from the dependency budget ADR-0009 (`platform-paths-live-in-cli-spec-paths`) sets for `cli/spec` subpackages.

The one definition of a release version, and of when one version is newer than another, is the public
package `go.vipengele.org/cli/spec/version`, in the `cli/spec` module, built on
`golang.org/x/mod/semver`. It exports two pure functions from strings to a bool:

- `IsRelease(v)` reports whether `v` is a release version.
- `Newer(current, latest)` reports whether `latest` is a release version that sorts above `current`.

`buildinfo.Info.IsDev` in `cli/core` delegates to `IsRelease`. `update` calls `Newer` to decide whether
the latest tag a `Source` lists is an update for the running binary.

## The rule

A release version is `vMAJOR.MINOR.PATCH`, optionally with a prerelease (`v1.2.3`, `v1.2.3-rc.1`),
with no build metadata, that is not a Go pseudo-version. This is the rule of ADR-0007, which uses it
to classify the running binary. ADR-0007 says nothing about the other side of a comparison; applying
the same rule to the latest tag a `Source` reports is this ADR's extension of it.

A current version that is not a release version is a dev build, and a dev build is older than every
release version: `Newer` is true for any release `latest` when `current` is empty, `(devel)`, a
pseudo-version or carries build metadata. A `latest` that is not a release version is never newer.
Invalid input is not an error and never a panic: it means no update.

The reason for one definition is that "dev" and "newer" are decided in two places, the binary
reporting `dev` in `version --json` and `update` reading a tag. If either definition changes, it has
to change in one place, or a binary reports `dev: false` for a string that `update` treats as
unusable.

## The `golang.org/x/mod` dependency

`golang.org/x/mod` is the first third-party `require` of `cli/spec`. Its reach follows the import
graph:

- `cli/spec` requires it directly.
- `cli/core` requires it as an indirect dependency, because `buildinfo` imports `cli/spec/version`.
- `update` requires `cli/spec` and its `go.mod` lists no `golang.org/x/mod`. It takes the
  dependency, as an indirect require, when it first imports `cli/spec/version`.
- `daemon` requires no sibling and no module, and is not reached.
- `commons` is not reached.

## Considered options

- **The rule duplicated in `update`, with a mirrored test.** `update` may not import `cli/core`
  (ADR-0008, enforced by `depguard`), which is why the rule cannot simply stay in `buildinfo`. A copy
  drifts silently: nothing fails when one side changes, and the binary reports `dev: false` while
  `update` treats the same string as unusable.
- **`buildinfo` moved into `cli/spec`.** Both sides could then import it, but the package path is the
  linker path: every consumer's build passes `-X go.vipengele.org/cli/core/buildinfo.version=...`,
  and the move changes it, together with `testdata/ldflagsmain`, `ldflags_test.go`, ADR-0007's
  wording on who owns the contract and the **Build info** entry of `CONTEXT.md`. It also invites the
  shared Go type ADR-0007 rejects. Where build info lives is a separate question about the object
  model, not one this decision needs to answer.
- **A package named `release`.** It collides with the glossary term **Release**: one project at one
  version, identified by its prefixed tag. A release version here is a single version string.
- **Exporting `Compare` or a parsed `Version` type.** Nothing needs them. Adding an export later is a
  compatible change and removing one is not.

## What this changes in earlier ADRs

- **ADR-0003.** It lists "version comparison" among what `update` owns. The comparison rule is
  defined in `cli/spec/version`; `update` owns the decision to check and calls `Newer`. ADR-0003's
  line that `update` requires no sibling was already replaced by ADR-0005 and ADR-0008.
- **ADR-0009 (`platform-paths-live-in-cli-spec-paths`).** It holds `paths` to "no dependency beyond
  the standard library and `cli/spec`" and rejects `adrg/xdg` as "a third-party module in every
  consumer's graph for a few path rules". `cli/spec/version` has a third-party dependency, so that
  budget describes `paths` and not every subpackage of `cli/spec`. The cost is the one named
  above: a module that `cli/core` and, once it imports the package, `update` carry, and that
  `daemon` does not. It is accepted because the alternative to one rule is two copies of it, and
  because `golang.org/x/mod/semver` is the standard implementation of the ordering. ADR-0009's
  rejection of `adrg/xdg` stands for what it weighed: a few path rules that needed wrapping anyway.

## Consequences

- **`Newer` is pure ordering.** A prerelease sorts above the release before it, so
  `Newer("v1.2.3", "v1.3.0-rc.1")` is true. Whether a user on a stable version is offered a
  prerelease is the caller's decision, made by the update check.
- **A tag published without the `v` is never offered.** A `Source` tag `1.2.3` is not a release
  version, so `Newer` is false, and the caller cannot tell that from being up to date. A release
  tagged that way is silently never offered.
- A change to `cli/spec` selects `cli/core` and `update` in CI (ADR-0006).

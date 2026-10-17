# The default update source reads the GitHub latest redirect, then the API

Status: amends ADR-0003, which has `Config.Source` required: a nil `Source` resolves to the GitHub
release source, built from the tool's identity.

`update` needs the latest release of the tool's repository. `GitHubSource` is the `Source` for a
tool released on GitHub, and a `Config` whose `Source` is nil uses it, so a tool declares no source
unless its releases live elsewhere. `ErrNoSource` does not exist.

## The rules

- **The web redirect is read first.** A request to `/<owner>/<repo>/releases/latest` without
  following its redirect answers with a redirect to `/<owner>/<repo>/releases/tag/<tag>`; the tag
  and the page are the latest release. The web page has no API rate limit, so the check an
  unauthenticated tool makes at start-up does not spend the quota of a shared address.
- **The REST API is the fallback.** A response that is not a redirect to a release tag page, and a
  redirect request that fails for any reason other than a timeout or a cancellation, are followed by
  `/repos/<owner>/<repo>/releases/latest`. A timeout is not retried on the second host, and a
  cancellation is the caller's own doing.
- **`Release.Assets == nil` means the assets are not listed.** The redirect names a tag and no
  assets, so it returns a nil slice; the API path always returns a non-nil one, empty when the
  release has no assets. A caller tells a release with no assets from one whose assets were not
  listed by the nil.
- **A pre-release is decided by the tag.** A tag with a semver pre-release suffix (`v1.2.3-rc.1`)
  is a pre-release on both paths, and `Latest` returns `ErrPrerelease` unless `IncludePrereleases`
  is set. The `prerelease` flag of the API response is not read, so both paths agree.
- **`IncludePrereleases` skips the redirect.** The latest-release page and endpoint never name a
  pre-release, so `Latest` reads the first page of `/repos/<owner>/<repo>/releases?per_page=100`,
  drops drafts and releases whose tag is not a release version, and returns the highest tag in
  version order (`0010-version-comparison-lives-in-cli-spec-version.md`). A page with no such
  release is an error.
- **Errors are typed.** A non-success status is a `*StatusError` holding the URL, the code and the
  first kilobyte of the body; an expired deadline is a `*TimeoutError`, which satisfies `net.Error`
  and unwraps to the cause; a tag that is not a release version is a `*TagError`. A prefixed tag
  such as `source/update/v1.2.3` is a `*TagError`: a tool released from a repository of many
  projects does not use this source.

## Considered options

- **The REST API only.** It returns assets, but an unauthenticated address is limited to sixty
  requests an hour, shared by everyone behind it, and a start-up check would fail where the
  redirect does not.
- **Trusting the order of the release list, or `/releases/latest`, for pre-releases.** The list is
  ordered by creation date, so a patch release of an older line sorts above a newer line's
  pre-release, and `/releases/latest` never returns a pre-release. The highest tag in version
  order is the rule that does not depend on either.
- **Treating any redirect as a hit.** A redirect to a login page, a renamed repository or another
  path carries no tag. Only a `Location` that ends in `/<owner>/<repo>/releases/tag/<tag>` names a
  release; anything else falls back to the API.

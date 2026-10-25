# A missing asset on the redirect path surfaces at download

Status: extends ADR-0016, whose redirect path returns a release with no listed assets, and ADR-0003,
whose `update` project owns asset selection.

`Config.Select` names the archive and the checksums asset of a release for the tool's platform. A
release read through the web redirect (ADR-0016) names a tag and no assets, so `Select` cannot check
that the assets exist. They are checked when they are downloaded.

## The rules

- **`Release.Assets == nil` means the assets are not listed.** A release reached through the web
  redirect returns a nil slice; the API path returns a non-nil one, empty when the release has no
  assets.
- **A listed release is matched by name.** For a non-nil `Assets`, each name must match an asset
  exactly. An empty non-nil slice is a miss for both names, so `Select` returns a
  `*AssetNotFoundError` for each, joined when both are missing.
- **An unlisted release is located by the source.** A `Source` may implement the optional
  `AssetLocator`: `AssetURL(tag, name string) string`. For a nil `Assets`, `Select` builds each
  asset's URL through it. `GitHubSource` returns
  `<WebURL>/<owner>/<repo>/releases/download/<tag>/<name>`, with tag and name each escaped as a path
  segment. It makes no request.
- **`Size` 0 means unknown.** An asset `Select` locates carries `Size` 0 because the redirect does not
  report one. A consumer treats it as not reported, not as empty.
- **A source that cannot locate fails.** A nil `Assets` with a `Source` that is not an `AssetLocator`
  gives an error wrapping `ErrAssetsNotListed`.
- **A 404 on a located URL is a missing asset.** The download step maps a 404 from a URL `Select`
  built to a `*AssetNotFoundError` for that tag and name, not to a `*StatusError`. A caller sees one
  error for a missing asset on both paths.

## Considered options

- **Listing the assets through the REST API when `Assets` is nil.** It finds a missing asset in
  `Select`, but it spends the unauthenticated rate-limit quota that ADR-0016 reads the redirect first
  to protect, on every check.
- **A HEAD request per asset.** It finds a missing asset early, at the cost of two more requests per
  check and network I/O in what is otherwise a pure selection step with no `Config` seam of its own.

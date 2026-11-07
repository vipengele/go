---
about: "update's GitHub token rides a derived per-Latest API client whose transport adds it only for the APIURL host; downloads and the web probe keep the shared Config.HTTPClient, because one injected client serves both and a client-level credential would follow the asset redirect to the CDN"
saw:
  - source/update/githubauth.go
  - source/update/github.go
  - source/update/config.go
  - source/update/download.go
  - docs/adr/0028-github-api-requests-carry-a-token-kept-to-the-api-host-and-revalidate-with-etags.md
---

- `Config.resolve` (source/update/config.go, the `if r.Source == nil` block) builds the default
  `GitHubSource` over the same `HTTPClient` that `Config.get` (source/update/download.go) uses for
  asset downloads, and GitHub asset URLs redirect to a CDN host. A transport or header set on that
  shared client would therefore reach the download hosts.
- So `GitHubSource.apiClient` (source/update/githubauth.go) builds, for each `Latest`, a shallow
  copy of the injected client: `tokenTransport` adds `Authorization: Bearer` on a clone of the
  request only when the host equals the `APIURL` host (case-insensitive, port included), and
  `CheckRedirect` refuses any other host with `*CrossHostRedirectError`. The host check is the
  guarantee — a transport sees every redirect hop — and the separate client keeps download.go free
  of auth code (ADR-0028).
- The token is read once per `Latest`, and only on the REST API path: `latestByAPI` and
  `highestListed` (source/update/github.go) each build the `apiClient` once and pass it to `apiGet`
  for every request, every page of the release list included; the web redirect probe never calls
  `Token`. Tests build `GitHubSource`
  by struct literal, so nothing is precomputed in `NewGitHubSource`.
- The ETag cache is a separate file, `github-cache.json` (`NewFileResponseCache`,
  source/update/githubcache.go), not part of `State`: a custom `StateStore` would not carry new
  fields (ADR-0023, ADR-0028).

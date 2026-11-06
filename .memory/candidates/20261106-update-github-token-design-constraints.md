---
about: "update shares one http.Client between GitHubSource and asset downloads, so a token must be a per-request header on the API path, never a Transport/client-level credential"
saw:
  - source/update/config.go
  - source/update/github.go
  - source/update/download.go
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0016-the-default-update-source-reads-the-github-latest-redirect-then-the-api.md
  - docs/adr/0023-update-keeps-its-check-state-through-cli-state.md
---

Found while scoping issue #62 (GitHub token, ETags, typed rate-limit error). No token, ETag or rate-limit code exists yet: `grep -rn 'Authorization|ETag|If-None-Match|RateLimit|GH_TOKEN|GITHUB_TOKEN' source/update` -> 0 hits (only ADR-0003:57 sketches `Token func(ctx) (string, error)` on Config).

- `Config.resolve` (config.go:455-460) sets one `HTTPClient` and builds the default `GitHubSource` over that same client. `Config.get` (download.go:278-287) downloads with `r.HTTPClient.Do` and the default redirect policy; it sets no headers. GitHub asset URLs redirect github.com -> a CDN host, so an Authorization added by a RoundTripper on the shared client would reach the download hosts (Go only strips it on cross-host redirects when set as a request header). The token must be set in `GitHubSource.get` (github.go:314-335, next to User-Agent/Accept) and only for APIURL requests; the web redirect request (WebURL, github.go:~147) needs none (ADR-0016: web path has no rate limit).
- Library vs env: update reads env only through the `Config.Getenv` seam (config.go:149, nil -> os.Getenv; used for paths and `<EnvPrefix>_NO_UPDATE_CHECK`, check.go:39,57). Reading GH_TOKEN/GITHUB_TOKEN would go through that seam, not os.Getenv directly. gochecknoglobals is enabled (source/update/.golangci.yml).
- Typed errors: non-2xx is `*StatusError` (errors.go:24, Body capped 1 KiB); a rate-limit error should be a new typed error or wrap it (ADR-0016 "Errors are typed").
- ETag cache: the only persisted state is `State{LastAttempt, LatestVersion}` via `StateStore` in `update-state.json` (ADR-0023; state.go), no lock, last write wins, corrupt/missing loads empty. Adding ETag + body there means changing the State JSON shape; no format version field was observed.

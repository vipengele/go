---
about: "update's depguard denies only cli/core and cobra, so new sibling imports (httpclient/github/resilience) are lint-legal, but ADR-0008 says update requires only cli/spec and commons"
saw:
  - source/update/.golangci.yml
  - source/update/github.go
  - source/update/githubauth.go
  - source/update/download.go
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
  - docs/adr/0028-github-api-requests-carry-a-token-kept-to-the-api-host-and-revalidate-with-etags.md
---

HTTP lives in update/github.go (latestByRedirect, latestByAPI, highestListed, listPage, apiGet, get, rateLimitError
~594, retryAfter, readLimited), githubauth.go (token transport), githubcache.go (ETag file cache), download.go
(fetchBounded, archive stream with MaxDownloadBytes), checksums.go (1 MiB cap). update/.golangci.yml has only deny
rules (cli/core, cobra), so no lint block on new siblings. The real constraint is ADR-0008 line 17: "update requires
cli/spec and commons, and no other sibling" -- must be amended. daemon requires update (ADR-0033: no project
requires daemon), and tools pin release-tool.yml at the update tag, so update's go.mod ripples into daemon and
every tool. ADR-0028 (github-api) invariants: token only when host == APIURL host, clone request, CheckRedirect
refuses cross-host (*CrossHostRedirectError), 401 with token is *TokenRejectedError (no anonymous retry),
403/429 -> *RateLimitError, caches keyed per page URL, 8/32 MiB read caps, web redirect probe and downloads never
carry the token, 304 decoded as 200.

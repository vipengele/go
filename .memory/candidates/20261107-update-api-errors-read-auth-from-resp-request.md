---
about: "apiStatusError decides 'authenticated' (TokenRejectedError on 401, RateLimitError.Authenticated) from resp.Request's Authorization header, so the token transport must pass the cloned, header-carrying request to its base transport"
saw:
  - source/update/github.go
  - source/update/githubauth.go
---

- `GitHubSource.apiStatusError` (source/update/github.go) computes
  `authenticated := resp.Request != nil && resp.Request.Header.Get("Authorization") != ""`. A 401
  becomes `*TokenRejectedError` only when that holds, and `RateLimitError.Authenticated` (which
  decides whether the error text mentions the 60-per-hour anonymous limit) comes from it too.
- `resp.Request` is the request the base `RoundTripper` received. `tokenTransport.RoundTrip`
  (source/update/githubauth.go) clones the request, sets the header on the clone and hands the
  clone to the base transport, so the response carries it. A transport that set the header on
  some other request, or stripped it before the base transport, would make every 401 a plain
  `*StatusError` and every rate-limit error read as unauthenticated, with no compile error.
- Classification lives only at the REST API call sites; the shared `statusError` used by
  download.go is untouched, so a download 429 stays a `*StatusError`.

---
about: "how the update/updatetest fake release behaves per serving mode and fault, and what it cannot express, so a test author knows when to keep a hand-rolled handler"
saw:
  - source/update/updatetest/server.go
  - source/update/updatetest/options.go
  - source/update/download_test.go
  - source/update/github_test.go
---

Found while building the kit and moving the updater's own tests onto it (issue vipengele/go#57).

- Both web and API routes are served from one address. By default `GitHubSource` takes the redirect
  (`TargetLatest`, 302) and gets no asset list, so `Select` locates the assets and a missing one
  surfaces at `Download` as a located 404. `APIOnly()` makes the web page answer 404, so the source
  falls back to the REST API and `Select` rejects a missing asset from the list before any download
  (server.go `serveLatest`, `serveAPI`; download_test.go `TestDownloadMissingAsset` asserts 0/0
  requests in API mode, 1/1 or 1/0 in redirect mode).
- `MissingAsset(name)` acts in both modes at once: 404 on the download URL and omitted from the API
  asset list. A missing archive stays in the checksums file (options.go).
- `Slow(target, gate)` blocks before any header is written; a request released by its context or by
  server cleanup gets no response (`panic(http.ErrAbortHandler)` closes the connection), because a
  plain return would answer an empty 200 (server.go `serve`). `Blocked(target)` closes once a request
  waits, so a test cancels or expires its context on that signal instead of sleeping.
- The kit always sets `Content-Length`, so after a failed checksum the last streaming progress call
  already reads (n, n); a test cannot look for a final done == total call and checks for a repeated
  count instead (download_test.go `TestDownloadBadChecksum`).
- The kit cannot express: the release-list route used with `IncludePrereleases`, a chosen status or
  error body (an unlocated 404), a mid-body stall, request-header inspection, or a timed delay. Tests
  of those keep their own handlers in github_test.go and download_test.go.
- A request path is matched on `r.URL.EscapedPath()`, so a tag containing `/` (`source/x/v1.2.3`)
  routes correctly (server.go `serve`).

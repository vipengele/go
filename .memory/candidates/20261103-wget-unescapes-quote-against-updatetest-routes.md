---
about: "GNU wget sends %27 in a URL path as a literal quote, and updatetest routes on the exact EscapedPath, so a wget test with ' in an asset name sees a 404 first"
saw:
  - source/update/updatetest/server.go
  - source/update/installscript/render.go
---

- `render.go` builds the download URL from `url.PathEscape` of each literal run, so `'` is
  sent as `%27`.
- GNU wget rewrites `%27` to `'` before sending. `updatetest/server.go:213` looks the route up by
  `r.URL.EscapedPath()`, which then differs from the registered path: the first request 404s and the
  server logs "updatetest: unexpected request"; wget retries with the original escaping and succeeds.
- curl sends the path as given. Shell tests with a quote in an asset name use curl.

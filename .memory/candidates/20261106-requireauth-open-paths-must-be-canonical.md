---
about: "httpserver.RequireAuth skips authentication only for a canonical path (empty RawPath, already clean); matching the cleaned path lets a router that routes on the raw path serve a protected route unauthenticated"
saw:
  - source/daemon/httpserver/auth.go
  - source/daemon/httpserver/auth_test.go
  - docs/adr/0026-the-http-server-kit-has-a-fixed-middleware-order-and-pluggable-auth.md
---

- `RequireAuth` (source/daemon/httpserver/auth.go) lets a request through without credentials only when `canonical(r.URL)` — `r.URL.RawPath == ""` and `cleanPath(r.URL.Path) == r.URL.Path` — and the path matches an unauthenticated entry ServeMux-style.
- Matching on `cleanPath(r.URL.Path)` instead is a bypass: `New` accepts any `http.Handler`, and a router such as chi routes on `RawPath` when it is set, so `/x%2F..%2Flivez` (Path `/x/../livez`, cleaned `/livez`) would skip auth and reach a protected `/{id}` route. `TestRequireAuthEncodedSlashCannotReachAnOpenPath` in auth_test.go pins it.
- Because RequireAuth wraps the router, a non-canonical request such as `/x/../livez` gets the 401 before an `http.ServeMux` could redirect it: a probe without credentials has to request the canonical path.

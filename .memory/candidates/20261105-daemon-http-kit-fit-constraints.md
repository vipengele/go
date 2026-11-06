---
about: "How the daemon HTTP Runner and the httpserver kit divide the work: the Runner owns serving, BaseContext and the TLS switch; httpserver builds the *http.Server; depguard confines sibling imports; logging is an injected *slog.Logger"
saw:
  - source/daemon/http.go
  - source/daemon/health_http.go
  - source/daemon/option.go
  - source/daemon/httpserver/server.go
  - source/daemon/.golangci.yml
  - source/daemon/AGENTS.md
  - source/daemon/doc.go
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/release-notes/commons/v0.1.0.md
---

- Runner: `NewHTTPServer(srv *http.Server, ...HTTPOption)` binds srv.Addr in Start; `NewHTTPServerListener(srv, l)` serves a caller-bound listener (http.go). The Runner owns Serve/ServeTLS/Shutdown/Close; the caller owns constructing the server and the listener. Start overwrites `srv.BaseContext` (http.go, Start), so `httpserver.New` never sets it. Stop = Shutdown(ctx) then Close, base ctx cancelled after drain.
- TLS switch: `srv.TLSConfig != nil` makes Start serve with `ServeTLS(l, "", "")`; a config with none of Certificates, GetCertificate, GetConfigForClient fails Start with `ErrNoCertificate` before binding (http.go, `hasCertificate`). Stated in daemon doc.go `# HTTP` and ADR-0026.
- Option pattern: `HTTPOption` interface with unexported `applyHTTP`; `OrderOption` (WithOrder) is shared. Supervisor `Option` and `httpserver.Option` are `func(*config) error`-style, errors prefixed with the package name.
- `(*Health).Handler` (health_http.go) sets `Cache-Control: no-store` and `X-Content-Type-Options: nosniff` and writes JSON via `spec.WriteJSON` (first key "schema"); it does no auth: probe access is the consumer's (ADR-0025), which `httpserver.WithUnauthenticatedPaths` provides.
- Lint: `.golangci.yml` = standard + gochecknoglobals (also in _test.go) + depguard: outside daemoncmd/ only `daemon`, `daemon/service`, `daemon/httpserver`, `cli/spec`, `cli/spec/paths`, `filelock` may be imported among siblings; cobra and cli/core banned. No package-level vars except sentinel errors.
- Logging: the root daemon package imports no `slog`; `httpserver` takes an injected `*slog.Logger` (WithLogger) that defaults to discarding (server.go), since logging is cli/core's opt-in (ADR-0018).
- ADR-0003 names "the HTTP server kit" as part of daemon's scope; it is the package `go.vipengele.org/daemon/httpserver`, not a project.

---
about: "Constraints on adding an HTTP server kit (issue #73) to daemon: the runner takes a caller-built *http.Server and overwrites its BaseContext, depguard confines sibling imports, no slog anywhere in daemon, no daemon release yet"
saw:
  - source/daemon/http.go
  - source/daemon/health_http.go
  - source/daemon/option.go
  - source/daemon/.golangci.yml
  - source/daemon/AGENTS.md
  - source/daemon/doc.go
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/release-notes/commons/v0.1.0.md
---

Memory held nothing on tumika's API layer (searches for tumika/api/middleware/allowlist/origin matched no note or candidate).

- Runner: `NewHTTPServer(srv *http.Server, ...HTTPOption)` binds srv.Addr in Start; `NewHTTPServerListener(srv, l)` serves a caller-bound listener (http.go). The Runner owns Serve/Shutdown/Close; the caller owns constructing the server and the listener. Start overwrites `srv.BaseContext` (http.go, Start), so a kit must not rely on setting it. Stop = Shutdown(ctx) then Close, base ctx cancelled after drain. A kit that only returns a configured `*http.Server` + `http.Handler` fits without changing the runner.
- Option pattern: `HTTPOption` interface with unexported `applyHTTP`; `OrderOption` (WithOrder) is shared. Supervisor `Option` is `func(*Supervisor) error` (option.go), errors prefixed `daemon: ` and wrapped with %w.
- Existing precedent in `(*Health).Handler` (health_http.go): sets `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`, 404/405 with Allow, JSON via `spec.WriteJSON` envelope (first key "schema"). No middleware, no auth: "decides who may reach it" is left to the consumer.
- Lint: `.golangci.yml` = standard + gochecknoglobals (also in _test.go) + depguard: outside daemoncmd/ only `daemon`, `daemon/service`, `cli/spec`, `cli/spec/paths`, `filelock` may be imported among siblings; cobra and cli/core banned. No package-level vars except sentinel errors.
- No `slog` import exists anywhere in source/daemon (grep). Logging is cli/core's opt-in (ADR-0018), so a kit's logger must be injected (e.g. *slog.Logger option) rather than use a daemon-wide convention.
- ADR-0003 names "the HTTP server kit" as part of daemon's scope.
- Release: docs/release-notes/ holds only commons/v0.1.0.md; no docs/release-notes/daemon exists, so daemon has no recorded release (git tags not checkable: /home/user/go is not a git repo here). Verify tags before claiming nothing is released.

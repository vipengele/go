# The HTTP server kit has a fixed middleware order and pluggable authentication

Status: accepted. It decides the HTTP hardening that
`docs/adr/0021-a-runner-starts-when-ready-and-reports-its-exit-through-done.md` leaves out. The
Runner contract of that ADR and the health endpoints of
`docs/adr/0025-health-is-checked-on-demand-with-distinct-liveness-and-readiness.md` stand.

A daemon that serves an API needs the same protections each time: a panic must not take the process
down, a rebound hostname or a foreign web page must not reach it, a caller must prove who it is,
and a slow or oversize request must not hold it. `go.vipengele.org/daemon/httpserver` builds that
`*http.Server`, which the HTTP Runner then serves. ADR-0003 assigns the HTTP server kit to the
`daemon` project, so it is a package of `daemon`, not a project of its own.

## The rule

- **Fixed middleware order.** `New` wraps the handler, outermost first, in Recover, Log,
  SecureHeaders, AllowHosts, AllowOrigins, RequireAuth (only with an authenticator) and LimitBody.
  Recover is outermost so a panic anywhere is caught; it logs the panic itself, so a panicking
  request is logged by Recover and not by Log. Log is outside every rejection, so refused requests
  are logged. SecureHeaders is outside the checks, so every response, rejections included, carries
  its headers. The host and origin checks run before authentication, so a request from a rebound
  host or a foreign origin is refused before a credential is looked at. LimitBody is innermost: the
  body of a request refused earlier is never read. The middleware are exported for a server
  assembled by hand; `New` offers no way to reorder or drop one.
- **Authenticator.** `Authenticator` is an interface of `Authenticate(*http.Request) error` and
  `Challenge() string`, the value of the `WWW-Authenticate` header of a 401. `BearerToken` is built
  in: it reads the token from a func on every request, so a rotated token applies at once, and
  compares SHA-256 digests in constant time, so neither content nor length leaks through timing. An
  empty token rejects every request, so a daemon that has not been given a token fails closed. The
  interface exists because a daemon's callers are not always bearer clients (a mutual-TLS identity,
  a signed request); `WithAuthenticator` takes any implementation and `WithBearerTokenFunc` is the
  common case.
- **Unauthenticated paths.** `WithUnauthenticatedPaths` lists the paths that skip authentication,
  matched as `http.ServeMux` matches a pattern, on the cleaned path, so `/x/../livez` cannot reach
  a protected handler as an open one. Probe authentication is the consumer's, as ADR-0025 states:
  these paths are how a consumer exposes `/livez` and `/readyz` to an orchestrator while the rest
  of the API stays protected.
- **Hosts and origins.** `localhost` and every IP literal are always allowed, because DNS rebinding
  works through a hostname that a browser resolves to the server's address; a request addressed to
  an IP literal cannot be a rebinding. Any other name has to be listed with `WithAllowedHosts`, and
  a Host that is not allowed is answered 421. A request with no `Origin` header, which a client that
  is not a browser sends, passes; one with an `Origin` has to match a listed origin exactly, and
  `null` never matches. The answer is 403. No CORS header is written: an allowed cross-origin
  request is served and the browser withholds the response from the page.
- **TLS.** `WithTLS` sets the server's `TLSConfig` to a clone with `MinVersion` raised to TLS 1.2.
  The HTTP Runner serves TLS with `ServeTLS` when `TLSConfig` is non-nil and fails `Start` with
  `ErrNoCertificate` when that configuration holds no certificate, so a server cannot silently
  fall back to plain HTTP. A `TLSConfig` on the server is thus part of the documented contract of the
  Runner, stated in `# HTTP` of the `daemon` package documentation.
- **Plain-HTTP exposure warning.** `New` logs a warning when the configured address is reachable
  beyond loopback and there is no TLS; `CheckListener` makes the same check on the address of a
  bound listener, which `:0` and a name only reveal once bound. It warns and does not refuse, as a
  daemon behind a TLS-terminating proxy is legitimate.
- **Logger.** The logger defaults to one that discards. A library that writes to the process's
  default logger unasked decides a consumer's logging for it; `WithLogger` sets the logger of
  every middleware and of the server's `ErrorLog`.
- **Limits.** The defaults are a 5 second read-header timeout, 30 seconds to read and to write, 120
  seconds idle, 64 KiB of headers and a 1 MiB body. A handler that streams longer than the write
  timeout raises it with `WithTimeouts` or sets a deadline per request with
  `http.ResponseController.SetWriteDeadline`.
- **Errors.** Every rejection is the JSON envelope of `spec.WriteJSON` at schema 1, with a stable
  `code`, so a client branches on the code and not on the message. `DecodeJSON` rejects a wrong
  media type, an unknown field, trailing data and an oversize body with a `*RequestError`.

## Rejected alternatives

- **A configurable or reorderable chain.** Each position is a security property: authentication
  before the body is read, the host check before authentication, headers on every rejection. A
  chain a consumer arranges can put authentication outside Recover, or the body limit ahead of the
  checks, and nothing flags it. A consumer who needs a different chain assembles the exported
  middleware by hand.
- **A bearer-only API with no interface.** It would make the first non-bearer caller replace the
  kit rather than plug into it.
- **Serving TLS from a separate Runner, or with certificate-file options.** Certificate loading and
  reloading is the consumer's, through `tls.Config` and its `GetCertificate`; a second Runner for
  the same listener would split one server across two lifecycles. `TLSConfig` already exists on
  `http.Server`, so it is the switch.
- **CORS support.** A daemon API is called by tools, not by web pages. Origin checking refuses the
  browser; it adds no header that would let one read a response.
- **The kit as its own project.** It exists to be served by the daemon's HTTP Runner and is
  versioned with it; a project of its own would add a release and a `go.mod` to keep in step for
  one consumer, where ADR-0003 lists the HTTP server kit among what `daemon` owns.

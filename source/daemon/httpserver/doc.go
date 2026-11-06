// Package httpserver builds the *http.Server a daemon serves its API with: New wraps a handler in
// a fixed chain of hardening middleware, sets conservative timeouts and limits, and leaves the
// serving to the daemon runner (daemon.NewHTTPServer or daemon.NewHTTPServerListener). The
// middleware are exported for a server assembled by hand.
//
// # Middleware order
//
// New installs the middleware in one order, outermost first:
//
//	Recover → Log → SecureHeaders → AllowHosts → AllowOrigins → RequireAuth → LimitBody → handler
//
// RequireAuth is present only when an authenticator is configured. The order is not configurable,
// and each position has a reason:
//
//   - Recover is outermost, so a panic in any other middleware or in the handler is caught. It
//     logs the panic itself and answers 500 internal_error, so a panicking request is logged by
//     Recover, not by Log: the panic unwinds through Log without Log reaching its line.
//   - Log sits inside Recover and outside every rejection, so a request refused by a host,
//     origin or authentication check is logged with its status.
//   - SecureHeaders is outside the checks, so every response, rejections included, carries
//     "Cache-Control: no-store" and "X-Content-Type-Options: nosniff".
//   - AllowHosts and AllowOrigins run before authentication: a request from a rebound host or a
//     foreign origin is refused before any credential is looked at.
//   - LimitBody is innermost, so the body of a request refused earlier is never read and only an
//     authenticated request, or one for an unauthenticated path, gets a body reader.
//
// # Authentication
//
// An Authenticator decides whether a request carries valid credentials: Authenticate returns nil
// or an error that never contains a credential, and Challenge is the value of the
// WWW-Authenticate header sent with a rejection. WithAuthenticator installs one;
// WithBearerTokenFunc installs BearerToken, which matches "Authorization: Bearer <token>", the
// scheme case-insensitively, against the token a func returns on every request, so a rotated
// token takes effect at once. The comparison is of SHA-256 digests in constant time, so neither
// the content nor the length of the expected token leaks through timing. An empty token, or a nil
// func, rejects every request.
//
// RequireAuth answers 401 unauthorized with the challenge to a request the authenticator rejects.
// The paths of WithUnauthenticatedPaths skip authentication, which is how a daemon exposes probes
// such as /livez. They are matched as http.ServeMux matches a pattern, on the cleaned path: an
// entry without a trailing slash matches that path exactly, one with a trailing slash matches
// everything below it, and "/x/../livez" is cleaned to "/livez" before it is matched.
//
// # Hosts and origins
//
// AllowHosts is the defence against DNS rebinding, which needs a hostname that a browser resolves
// to the server's address. The host "localhost" and every IP literal, IPv4 or bracketed IPv6,
// with any port, are therefore always allowed; a name has to be listed with WithAllowedHosts. A
// listed name without a port matches any port or none, one with a port matches only that port.
// Any other Host is answered 421 misdirected_request.
//
// AllowOrigins answers 403 forbidden_origin to a request whose Origin header is present and not
// listed by WithAllowedOrigins. A request with no Origin header, as a client that is not a browser
// sends, passes. A listed origin matches exactly, scheme and host case-insensitively and the port
// exactly; the opaque origin "null" never matches. No CORS header is written: an allowed
// cross-origin request is served, and the browser withholds the response from the page.
//
// # TLS
//
// WithTLS sets the TLSConfig of the server to a clone of the configuration given, with MinVersion
// raised to TLS 1.2 when lower. The daemon runner serves TLS when the server's TLSConfig is set,
// and fails Start with daemon.ErrNoCertificate when that configuration holds no certificate. A
// server with no TLSConfig serves plain HTTP. New logs a warning when the address of WithAddr is
// reachable beyond loopback and there is no TLS; CheckListener makes the same check on the
// address a bound listener reports.
//
// # Errors
//
// Error answers with a status and the JSON envelope
// {"schema":1,"error":{"code":"...","message":"..."}}, written by spec.WriteJSON, with
// "Cache-Control: no-store" and "X-Content-Type-Options: nosniff". The middleware use the codes
// internal_error (500), unauthorized (401), misdirected_request (421) and forbidden_origin (403).
//
// DecodeJSON decodes a request body into a value and rejects a wrong media type, an unknown
// field, trailing data and a body past the limit. Each failure is a *RequestError that names the
// status and code to answer: unsupported_media_type (415), body_too_large (413) or bad_request
// (400).
//
// # Timeouts
//
// The server has ReadHeaderTimeout DefaultReadHeaderTimeout, ReadTimeout DefaultReadTimeout,
// WriteTimeout DefaultWriteTimeout and IdleTimeout DefaultIdleTimeout, a header limit of
// DefaultMaxHeaderBytes and a body limit of DefaultMaxBodyBytes. WithTimeouts, WithMaxBodyBytes
// change them. A handler that streams a response for longer than the WriteTimeout needs a longer
// one from WithTimeouts, or a deadline of its own per request with
// http.ResponseController.SetWriteDeadline.
package httpserver

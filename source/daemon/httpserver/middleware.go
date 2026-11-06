package httpserver

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"time"
)

// Recover returns middleware that recovers a panic in the next handler. A panic with
// http.ErrAbortHandler is re-raised unchanged, so the server aborts the response silently. Any
// other panic is logged at Error level with the panic value, the stack, the method and the cleaned
// path, and then answered: with 500 internal_error when nothing of the response has been sent, its
// headers replaced by the envelope's; otherwise by panicking with http.ErrAbortHandler, since a
// response whose status is already sent cannot be corrected and must not look complete. A nil
// logger logs to slog.Default().
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &recoverWriter{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.Error("http handler panic",
					slog.String("method", r.Method),
					slog.String("path", cleanPath(r.URL.Path)),
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)
				if rw.written {
					panic(http.ErrAbortHandler)
				}
				clear(w.Header())
				Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// isFinal reports whether code ends the header phase of a response. An informational 1xx status
// other than 101 Switching Protocols is sent ahead of the final one and leaves it open.
func isFinal(code int) bool {
	return code >= 200 || code == http.StatusSwitchingProtocols
}

// recoverWriter records whether any of the response has been sent. Flushing and hijacking count as
// sent; both go through the wrapper, so http.NewResponseController reaches them through it.
type recoverWriter struct {
	http.ResponseWriter
	written bool
}

func (w *recoverWriter) WriteHeader(code int) {
	if isFinal(code) {
		w.written = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recoverWriter) Write(p []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(p)
}

func (w *recoverWriter) FlushError() error {
	w.written = true
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *recoverWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.written = true
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *recoverWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Log returns middleware that logs one Info line per request once the next handler returns: the
// method, the cleaned path, the status and the duration. The status is 200 when the handler sets
// none, as the server sends it. Neither the query string nor any header is logged, so neither can
// leak a credential such as the Authorization value. A nil logger logs to slog.Default().
func Log(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(rw, r)
			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", cleanPath(r.URL.Path)),
				slog.Int("status", status),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// statusWriter records the final status of a response; 0 means none has been sent.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 && isFinal(code) {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) FlushError() error {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// SecureHeaders returns middleware that sets "Cache-Control: no-store" and
// "X-Content-Type-Options: nosniff" on every response before calling the next handler, which may
// override either.
func SecureHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			next.ServeHTTP(w, r)
		})
	}
}

// AllowHosts returns middleware that answers 421 misdirected_request to a request whose Host is
// not allowed, the defence against DNS rebinding. Allowed are "localhost", any IPv4 literal and
// any bracketed IPv6 literal ("[::1]"), each with any port or none, and the hosts listed. A listed
// entry without a port ("example.com") matches that name with any port or none; one with a port
// ("example.com:8443") matches only that port. Names compare case-insensitively. An entry that is
// not a name or name:port, such as an empty string or one with an empty port, matches nothing.
func AllowHosts(hosts ...string) func(http.Handler) http.Handler {
	type allowed struct {
		name string
		port string // empty matches any port
	}
	list := make([]allowed, 0, len(hosts))
	for _, h := range hosts {
		name, port, bracketed, ok := splitHost(h)
		if !ok || bracketed || (strings.Contains(h, ":") && port == "") {
			continue
		}
		list = append(list, allowed{name: strings.ToLower(name), port: port})
	}

	permitted := func(host string) bool {
		name, port, bracketed, ok := splitHost(host)
		if !ok {
			return false
		}
		if bracketed {
			addr, err := netip.ParseAddr(name)
			return err == nil && addr.Is6()
		}
		if addr, err := netip.ParseAddr(name); err == nil {
			return addr.Is4()
		}
		name = strings.ToLower(name)
		if name == "localhost" {
			return true
		}
		for _, a := range list {
			if a.name == name && (a.port == "" || a.port == port) {
				return true
			}
		}
		return false
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !permitted(r.Host) {
				Error(w, http.StatusMisdirectedRequest, "misdirected_request", "host is not served here")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// splitHost splits a Host value into its host and port, the port empty when absent. bracketed
// reports a "[...]" host, returned without its brackets. ok is false for an empty host, a port
// that is not all digits, text after the closing bracket other than a port, and an unbracketed
// host holding more than one colon, as an IPv6 literal must be bracketed in a Host.
func splitHost(hostport string) (host, port string, bracketed, ok bool) {
	rest := hostport
	if strings.HasPrefix(rest, "[") {
		inner, after, found := strings.Cut(rest[1:], "]")
		if !found {
			return "", "", false, false
		}
		host, rest, bracketed = inner, after, true
		if rest != "" {
			if rest[0] != ':' {
				return "", "", false, false
			}
			port = rest[1:]
		}
	} else {
		switch strings.Count(rest, ":") {
		case 0:
			host = rest
		case 1:
			host, port, _ = strings.Cut(rest, ":")
		default:
			return "", "", false, false
		}
	}
	if host == "" || !isDigits(port) {
		return "", "", false, false
	}
	return host, port, bracketed, true
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// AllowOrigins returns middleware that answers 403 forbidden_origin to a request whose Origin
// header is present and not listed. A request without an Origin header, as sent by a client that
// is not a browser, passes. Each entry is a serialised origin, "scheme://host[:port]", and matches
// an Origin equal to it with the scheme and host compared case-insensitively and the port
// exactly, so "https://example.com" and "https://example.com:443" are different entries. The
// opaque origin "null" never matches, even when listed. No CORS header is written: an allowed
// cross-origin request is served, and the browser withholds the response from the page.
func AllowOrigins(origins ...string) func(http.Handler) http.Handler {
	list := make([]string, 0, len(origins))
	for _, o := range origins {
		if o = strings.ToLower(o); o != "null" {
			list = append(list, o)
		}
	}

	permitted := func(origin string) bool {
		origin = strings.ToLower(origin)
		for _, o := range list {
			if o == origin {
				return true
			}
		}
		return false
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			values := r.Header.Values("Origin")
			if len(values) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			if len(values) != 1 || !permitted(values[0]) {
				Error(w, http.StatusForbidden, "forbidden_origin", "origin is not allowed")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LimitBody returns middleware that limits the request body to n bytes with http.MaxBytesReader:
// a read past the limit fails with *http.MaxBytesError, which DecodeJSON answers as 413
// body_too_large, and the server closes the connection after the response.
func LimitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.MaxBytesHandler(next, n)
	}
}

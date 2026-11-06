package httpserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Authenticator decides whether a request carries valid credentials. Authenticate returns nil for
// an authenticated request and an error otherwise; the error never contains a credential.
// Challenge is the value of the WWW-Authenticate header sent with a rejection.
type Authenticator interface {
	Authenticate(r *http.Request) error
	Challenge() string
}

// BearerToken authenticates a request whose Authorization header is "Bearer <token>", the scheme
// matched case-insensitively (RFC 7235), against the token the func returns. The func is called
// on every request, so a rotated token takes effect at once. The comparison is of SHA-256 digests
// in constant time, so neither the content nor the length of the expected token leaks through
// timing. An empty expected token, or a nil func, rejects every request.
func BearerToken(token func() string) Authenticator {
	return bearerToken{token: token}
}

type bearerToken struct {
	token func() string
}

func (b bearerToken) Challenge() string { return "Bearer" }

func (b bearerToken) Authenticate(r *http.Request) error {
	if b.token == nil {
		return errors.New("no token is configured")
	}
	expected := b.token()
	if expected == "" {
		return errors.New("no token is configured")
	}

	header := r.Header.Get("Authorization")
	if header == "" {
		return errors.New("missing Authorization header")
	}
	scheme, presented, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return errors.New("authorization scheme is not Bearer")
	}
	presented = strings.TrimLeft(presented, " ")
	if presented == "" {
		return errors.New("missing bearer token")
	}

	want := sha256.Sum256([]byte(expected))
	got := sha256.Sum256([]byte(presented))
	if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		return errors.New("invalid bearer token")
	}
	return nil
}

// RequireAuth returns middleware that answers 401 unauthorized, with the header
// "WWW-Authenticate: <a.Challenge()>", to every request a rejects. A request skips authentication
// only when its path is canonical and matches one of unauthenticatedPaths. A path is canonical
// when r.URL.RawPath is empty, so no escape such as "%2F" makes the raw path differ from the
// decoded one, and the decoded path is already rooted and cleaned by path.Clean, a trailing slash
// kept. Matching then follows http.ServeMux: an entry without a trailing slash ("/livez") matches
// that path exactly, and an entry with one ("/probe/") matches itself and every path below it but
// not "/probe". Every other request is authenticated, so "/x/../livez", "//livez" and
// "/x%2F..%2Flivez" need credentials: a handler that routes on the raw path, or without cleaning
// it, would otherwise serve a protected route to a request matched as an open one. The check
// runs before next, so an http.ServeMux behind it never gets to redirect a non-canonical path:
// a client that sends no credentials has to request the canonical path itself.
func RequireAuth(a Authenticator, unauthenticatedPaths ...string) func(http.Handler) http.Handler {
	open := append([]string(nil), unauthenticatedPaths...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if canonical(r.URL) && matchesAny(r.URL.Path, open) {
				next.ServeHTTP(w, r)
				return
			}
			if err := a.Authenticate(r); err != nil {
				w.Header().Set("WWW-Authenticate", a.Challenge())
				Error(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// canonical reports whether u's path is one every handler sees alike: no escaping that differs
// from the default, and a decoded path that cleanPath leaves unchanged.
func canonical(u *url.URL) bool {
	return u.RawPath == "" && cleanPath(u.Path) == u.Path
}

// cleanPath returns p rooted and cleaned by path.Clean, keeping a trailing slash, as http.ServeMux
// cleans a request path before matching it.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if strings.HasSuffix(p, "/") && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}

func matchesAny(p string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/") {
			if strings.HasPrefix(p, pattern) {
				return true
			}
		} else if p == pattern {
			return true
		}
	}
	return false
}

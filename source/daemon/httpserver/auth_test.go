package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.vipengele.org/daemon/httpserver"
)

const secret = "s3cr3t-token"

func authRequest(header string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	return req
}

func TestBearerTokenAuthenticate(t *testing.T) {
	tests := []struct {
		name   string
		header string
		ok     bool
	}{
		{name: "missing header is rejected", header: "", ok: false},
		{name: "wrong scheme is rejected", header: "Basic " + secret, ok: false},
		{name: "scheme without token is rejected", header: "Bearer", ok: false},
		{name: "scheme with empty token is rejected", header: "Bearer ", ok: false},
		{name: "wrong token is rejected", header: "Bearer wrong", ok: false},
		{name: "token prefix is rejected", header: "Bearer " + secret[:4], ok: false},
		{name: "correct token is accepted", header: "Bearer " + secret, ok: true},
		{name: "scheme is case-insensitive", header: "bEaReR " + secret, ok: true},
	}
	a := httpserver.BearerToken(func() string { return secret })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.Authenticate(authRequest(tt.header))
			if (err == nil) != tt.ok {
				t.Fatalf("Authenticate() = %v, want ok = %v", err, tt.ok)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Errorf("error %q contains the token", err)
			}
		})
	}
}

func TestBearerTokenRotationTakesEffectAtOnce(t *testing.T) {
	var current atomic.Value
	current.Store("old-token")
	a := httpserver.BearerToken(func() string { return current.Load().(string) })

	if err := a.Authenticate(authRequest("Bearer old-token")); err != nil {
		t.Fatalf("old token before rotation: %v", err)
	}
	current.Store("new-token")
	if err := a.Authenticate(authRequest("Bearer old-token")); err == nil {
		t.Error("old token after rotation is accepted")
	}
	if err := a.Authenticate(authRequest("Bearer new-token")); err != nil {
		t.Errorf("new token after rotation: %v", err)
	}
}

func TestBearerTokenEmptyExpectedTokenRejectsEveryRequest(t *testing.T) {
	a := httpserver.BearerToken(func() string { return "" })
	for _, header := range []string{"", "Bearer ", "Bearer x"} {
		if err := a.Authenticate(authRequest(header)); err == nil {
			t.Errorf("Authenticate(%q) accepted with an empty expected token", header)
		}
	}
}

func TestBearerTokenNilFuncRejectsEveryRequest(t *testing.T) {
	a := httpserver.BearerToken(nil)
	if err := a.Authenticate(authRequest("Bearer " + secret)); err == nil {
		t.Error("Authenticate accepted with a nil token func")
	}
}

func TestBearerTokenChallenge(t *testing.T) {
	if got := httpserver.BearerToken(func() string { return secret }).Challenge(); got != "Bearer" {
		t.Errorf("Challenge() = %q, want %q", got, "Bearer")
	}
}

func serveAuth(t *testing.T, urlPath, header string) *httptest.ResponseRecorder {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(handlerRan))
	})
	h := httpserver.RequireAuth(
		httpserver.BearerToken(func() string { return secret }),
		"/livez", "/probe/",
	)(ok)
	req := authRequest(header)
	req.URL.Path = urlPath
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequireAuthUnauthenticatedPaths(t *testing.T) {
	tests := []struct {
		path string
		open bool
	}{
		{path: "/livez", open: true},
		{path: "/x/../livez", open: false},
		{path: "//livez", open: false},
		{path: "livez", open: false},
		{path: "/livezz", open: false},
		{path: "/livez/", open: false},
		{path: "/livez/x", open: false},
		{path: "/probe/", open: true},
		{path: "/probe/x", open: true},
		{path: "/probe/x/y", open: true},
		{path: "/probe/x/../y", open: false},
		{path: "/probe", open: false},
		{path: "/probe/./x", open: false},
		{path: "/probe/..", open: false},
		{path: "/probe/../jobs", open: false},
		{path: "/prober/", open: false},
		{path: "/", open: false},
		{path: "/jobs", open: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := serveAuth(t, tt.path, "")
			want := http.StatusUnauthorized
			if tt.open {
				want = http.StatusOK
			}
			if rec.Code != want {
				t.Errorf("status = %d, want %d", rec.Code, want)
			}
			if ran := rec.Body.String() == handlerRan; ran != tt.open {
				t.Errorf("handler ran = %t, want %t", ran, tt.open)
			}
		})
	}
}

func serveAuthURL(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(handlerRan))
	})
	h := httpserver.RequireAuth(
		httpserver.BearerToken(func() string { return secret }),
		"/livez", "/probe/",
	)(ok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestRequireAuthEncodedSlashCannotReachAnOpenPath(t *testing.T) {
	tests := []struct {
		target string
		open   bool
	}{
		{target: "/livez", open: true},
		{target: "/probe/x", open: true},
		{target: "/x%2F..%2Flivez", open: false},
		{target: "/x%2f..%2flivez", open: false},
		{target: "/probe%2Fx", open: false},
		{target: "/probe/x%2Fy", open: false},
		{target: "/probe/x%2F..%2F..%2Fjobs", open: false},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			rec := serveAuthURL(t, tt.target)
			want := http.StatusUnauthorized
			if tt.open {
				want = http.StatusOK
			}
			if rec.Code != want {
				t.Errorf("status = %d, want %d", rec.Code, want)
			}
			if ran := rec.Body.String() == handlerRan; ran != tt.open {
				t.Errorf("handler ran = %t, want %t", ran, tt.open)
			}
		})
	}
}

func TestRequireAuthPassesAnAuthenticatedRequest(t *testing.T) {
	rec := serveAuth(t, "/jobs", "Bearer "+secret)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != handlerRan {
		t.Errorf("body = %q, want the handler's %q", rec.Body.String(), handlerRan)
	}
}

func TestRequireAuthRejectionCarriesChallengeAndEnvelope(t *testing.T) {
	rec := serveAuth(t, "/jobs", "Bearer wrong")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer")
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"code": "unauthorized"`) {
		t.Errorf("body %q lacks the unauthorized code", body)
	}
	if strings.Contains(body, secret) || strings.Contains(body, "wrong") {
		t.Errorf("body %q echoes a credential", body)
	}
}

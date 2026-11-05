package daemon

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
)

// healthFor returns a Health on a fixed clock holding a passing liveness check and a readiness
// check that fails with readyErr when it is not empty, and a liveness check failing with liveErr
// likewise.
func healthFor(t *testing.T, liveErr, readyErr string) *Health {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 11, 4, 3, 22, 0, 0, time.UTC)}
	h := newHealth(clock.now)
	live, ready := Check(pass), Check(pass)
	if liveErr != "" {
		live = failWith(liveErr)
	}
	if readyErr != "" {
		ready = failWith(readyErr)
	}
	if err := h.Add("self", live, Liveness); err != nil {
		t.Fatal(err)
	}
	if err := h.Add("db", ready); err != nil {
		t.Fatal(err)
	}
	return h
}

func serve(h *Health, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHandlerProbes(t *testing.T) {
	tests := []struct {
		name            string
		liveErr, rdyErr string
		path            string
		code            int
		body            string
	}{
		{"livez ok", "", "", "/livez", 200, "ok\n"},
		{"livez with readiness failing", "", "x", "/livez", 200, "ok\n"},
		{"livez down", "x", "", "/livez", 503, "down\n"},
		{"readyz ok", "", "", "/readyz", 200, "ok\n"},
		{"readyz unready", "", "x", "/readyz", 503, "unready\n"},
		{"readyz down", "x", "", "/readyz", 503, "down\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(healthFor(t, tt.liveErr, tt.rdyErr), http.MethodGet, tt.path)
			if rec.Code != tt.code {
				t.Errorf("code = %d, want %d", rec.Code, tt.code)
			}
			if got := rec.Body.String(); got != tt.body {
				t.Errorf("body = %q, want %q", got, tt.body)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q", got)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q", got)
			}
		})
	}
}

func TestHandlerLivezRunsOnlyLivenessChecks(t *testing.T) {
	tests := []struct {
		name    string
		liveErr string
		code    int
		body    string
	}{
		{name: "liveness passes", code: 200, body: "ok\n"},
		{name: "liveness fails", liveErr: "stuck", code: 503, body: "down\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var live, both, ready atomic.Int32
			h := NewHealth()
			liveCheck := Check(pass)
			if tt.liveErr != "" {
				liveCheck = failWith(tt.liveErr)
			}
			mustAdd(t, h, "self", func(ctx context.Context) error {
				live.Add(1)
				return liveCheck(ctx)
			}, Liveness)
			mustAdd(t, h, "loop", func(context.Context) error {
				both.Add(1)
				return nil
			}, Liveness, Readiness)
			mustAdd(t, h, "db", func(context.Context) error {
				ready.Add(1)
				return errors.New("refused")
			}, Readiness)

			rec := serve(h, http.MethodGet, "/livez")

			if rec.Code != tt.code {
				t.Errorf("code = %d, want %d", rec.Code, tt.code)
			}
			if got := rec.Body.String(); got != tt.body {
				t.Errorf("body = %q, want %q", got, tt.body)
			}
			if got := live.Load(); got != 1 {
				t.Errorf("liveness check called %d times, want 1", got)
			}
			if got := both.Load(); got != 1 {
				t.Errorf("check of both kinds called %d times, want 1", got)
			}
			if got := ready.Load(); got != 0 {
				t.Errorf("readiness check called %d times, want 0", got)
			}
		})
	}
}

func TestHandlerStatusAlwaysOK(t *testing.T) {
	for _, tt := range []struct{ liveErr, rdyErr string }{{"", ""}, {"", "x"}, {"x", ""}} {
		rec := serve(healthFor(t, tt.liveErr, tt.rdyErr), http.MethodGet, "/status")
		if rec.Code != http.StatusOK {
			t.Errorf("code = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q", got)
		}
	}
}

func TestHandlerStatusBodyIsEnvelope(t *testing.T) {
	h := healthFor(t, "", "db <&> down")
	rec := serve(h, http.MethodGet, "/status")

	var want bytes.Buffer
	if err := spec.WriteJSON(&want, statusSchema, h.Snapshot(context.Background())); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != want.String() {
		t.Errorf("body differs from the envelope:\n%s\nwant:\n%s", rec.Body.String(), want.String())
	}

	const exact = `{
  "schema": 1,
  "status": "unready",
  "live": true,
  "ready": false,
  "started_at": "2026-11-04T03:22:00Z",
  "uptime_seconds": 0,
  "checks": [
    {
      "name": "db",
      "kinds": [
        "readiness"
      ],
      "status": "fail",
      "error": "db \u003c\u0026\u003e down",
      "duration_ms": 0
    },
    {
      "name": "self",
      "kinds": [
        "liveness"
      ],
      "status": "pass",
      "duration_ms": 0
    }
  ]
}
`
	if got := rec.Body.String(); got != exact {
		t.Errorf("body = %s\nwant %s", got, exact)
	}
}

func TestHandlerStatusSchemaIsFirstKey(t *testing.T) {
	rec := serve(NewHealth(), http.MethodGet, "/status")
	if got := rec.Body.String(); !strings.HasPrefix(got, "{\n  \"schema\": 1,\n  \"status\"") {
		t.Errorf("body = %q", got)
	}
}

func TestHandlerMethods(t *testing.T) {
	for _, path := range []string{"/livez", "/readyz", "/status"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			rec := serve(healthFor(t, "", ""), method, path)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: code = %d, want 405", method, path, rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
				t.Errorf("%s %s: Allow = %q", method, path, got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s %s: Cache-Control = %q", method, path, got)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("%s %s: X-Content-Type-Options = %q", method, path, got)
			}
		}
	}
}

func TestHandlerHeadHasHeadersAndNoBody(t *testing.T) {
	for _, path := range []string{"/livez", "/readyz", "/status"} {
		h := healthFor(t, "x", "")
		get := serve(h, http.MethodGet, path)
		head := serve(h, http.MethodHead, path)
		if head.Code != get.Code {
			t.Errorf("%s: HEAD code = %d, GET code = %d", path, head.Code, get.Code)
		}
		if head.Body.Len() != 0 {
			t.Errorf("%s: HEAD body = %q", path, head.Body.String())
		}
		for _, k := range []string{"Content-Type", "Content-Length", "Cache-Control", "X-Content-Type-Options"} {
			if head.Header().Get(k) != get.Header().Get(k) {
				t.Errorf("%s: HEAD %s = %q, GET %q", path, k, head.Header().Get(k), get.Header().Get(k))
			}
		}
	}
}

func TestHandlerUnknownPath(t *testing.T) {
	for _, path := range []string{"/", "/live", "/livez/", "/status/x", "/healthz"} {
		rec := serve(healthFor(t, "", ""), http.MethodGet, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: code = %d, want 404", path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", path, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", path, got)
		}
	}
}

func TestHandlerUsesRequestContext(t *testing.T) {
	h := NewHealth()
	var got context.Context
	if err := h.Add("ctx", func(ctx context.Context) error { got = ctx; return nil }); err != nil {
		t.Fatal(err)
	}
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
	h.Handler().ServeHTTP(httptest.NewRecorder(), req)
	if got == nil || got.Value(key{}) != "v" {
		t.Error("check context does not derive from the request context")
	}
}

func TestHandlerStatusAnswers500WhenTheSnapshotDoesNotMarshal(t *testing.T) {
	h := NewHealth()
	// Add rejects an unknown Kind, so the check is registered past it: a Kind outside Liveness and
	// Readiness is the one value a Snapshot cannot be marshalled with.
	h.checks["bad"] = registered{
		check: func(context.Context) error { return nil },
		kinds: []Kind{9},
		state: newCheckState(),
	}
	rec := serve(h, http.MethodGet, "/status")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
}

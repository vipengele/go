package httpserver_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.vipengele.org/daemon/httpserver"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func assertEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"code": "`+code+`"`) {
		t.Errorf("body %q lacks the code %q", body, code)
	}
}

func newLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestRecoverAnswers500BeforeAnythingIsWritten(t *testing.T) {
	logger, logs := newLogger()
	h := httpserver.Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Partial", "1")
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x/../jobs?q=1", nil))

	assertEnvelope(t, rec, http.StatusInternalServerError, "internal_error")
	if got := rec.Header().Get("X-Partial"); got != "" {
		t.Errorf("X-Partial = %q, want the handler's headers dropped", got)
	}
	out := logs.String()
	for _, want := range []string{"level=ERROR", "boom", "method=GET", "path=/jobs", "stack="} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q lacks %q", out, want)
		}
	}
}

func TestRecoverAbortsAResponseAlreadyStarted(t *testing.T) {
	for name, start := range map[string]func(http.ResponseWriter){
		"after WriteHeader": func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) },
		"after Write":       func(w http.ResponseWriter) { _, _ = w.Write([]byte("partial")) },
		"after Flush":       func(w http.ResponseWriter) { _ = http.NewResponseController(w).Flush() },
	} {
		t.Run(name, func(t *testing.T) {
			logger, logs := newLogger()
			h := httpserver.Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				start(w)
				panic("boom")
			}))
			rec := httptest.NewRecorder()

			got := recoverServe(h, rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if got != http.ErrAbortHandler {
				t.Fatalf("panic = %v, want http.ErrAbortHandler", got)
			}
			if strings.Contains(rec.Body.String(), "internal_error") {
				t.Errorf("body %q carries an envelope after the response started", rec.Body.String())
			}
			if !strings.Contains(logs.String(), "boom") {
				t.Errorf("log %q lacks the panic value", logs.String())
			}
		})
	}
}

func TestRecoverAnswersAfterAnInformationalStatus(t *testing.T) {
	h := httpserver.Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	if got := recoverServe(h, rec, httptest.NewRequest(http.MethodGet, "/", nil)); got != nil {
		t.Fatalf("panic = %v, want none", got)
	}
	if !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("body %q lacks the internal_error envelope", rec.Body.String())
	}
}

func TestRecoverRepanicsErrAbortHandlerUnchanged(t *testing.T) {
	logger, logs := newLogger()
	h := httpserver.Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	rec := httptest.NewRecorder()

	got := recoverServe(h, rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got != http.ErrAbortHandler {
		t.Fatalf("panic = %v, want http.ErrAbortHandler", got)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if logs.Len() != 0 {
		t.Errorf("log = %q, want nothing logged", logs.String())
	}
}

func TestRecoverPassesARequestThatDoesNotPanic(t *testing.T) {
	rec := httptest.NewRecorder()
	httpserver.Recover(nil)(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func recoverServe(h http.Handler, w http.ResponseWriter, r *http.Request) (v any) {
	defer func() { v = recover() }()
	h.ServeHTTP(w, r)
	return nil
}

func TestLogRecordsMethodPathStatusAndDuration(t *testing.T) {
	logger, logs := newLogger()
	h := httpserver.Log(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x/../jobs?token=querysecret", nil)
	req.Header.Set("Authorization", "Bearer "+secret)

	h.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	for _, want := range []string{"level=INFO", "method=POST", "path=/jobs ", "status=418", "duration="} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q lacks %q", out, want)
		}
	}
	for _, leak := range []string{secret, "querysecret", "token", "Authorization", "?"} {
		if strings.Contains(out, leak) {
			t.Errorf("log %q contains %q", out, leak)
		}
	}
	if n := strings.Count(out, "\n"); n != 1 {
		t.Errorf("log has %d lines, want 1", n)
	}
}

func TestLogStatusDefaultsTo200(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"nothing written": func(http.ResponseWriter, *http.Request) {},
		"body only":       func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) },
		"informational first": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
		},
	} {
		t.Run(name, func(t *testing.T) {
			logger, logs := newLogger()
			httpserver.Log(logger)(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			if !strings.Contains(logs.String(), "status=200") {
				t.Errorf("log %q lacks status=200", logs.String())
			}
		})
	}
}

func TestResponseControllerFlushesThroughTheWrappers(t *testing.T) {
	for name, mw := range map[string]func(http.Handler) http.Handler{
		"Log":     httpserver.Log(slog.New(slog.DiscardHandler)),
		"Recover": httpserver.Recover(slog.New(slog.DiscardHandler)),
	} {
		t.Run(name, func(t *testing.T) {
			var flushErr error
			h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				flushErr = http.NewResponseController(w).Flush()
			}))
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if flushErr != nil {
				t.Errorf("Flush() = %v, want nil", flushErr)
			}
			if !rec.Flushed {
				t.Error("recorder was not flushed")
			}
		})
	}
}

func TestSecureHeadersSetsHeadersOnEveryResponse(t *testing.T) {
	var seen http.Header
	h := httpserver.SecureHeaders()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen = w.Header().Clone()
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for k, v := range map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
		if got := seen.Get(k); got != v {
			t.Errorf("%s seen by the handler = %q, want %q", k, got, v)
		}
	}
}

func TestAllowHosts(t *testing.T) {
	tests := []struct {
		host string
		ok   bool
	}{
		{host: "localhost", ok: true},
		{host: "localhost:8080", ok: true},
		{host: "LocalHost:1", ok: true},
		{host: "127.0.0.1", ok: true},
		{host: "127.0.0.1:8080", ok: true},
		{host: "192.168.1.20:9", ok: true},
		{host: "[::1]", ok: true},
		{host: "[::1]:8080", ok: true},
		{host: "[2001:db8::1]:443", ok: true},
		{host: "api.example.com", ok: true},
		{host: "api.example.com:9000", ok: true},
		{host: "API.Example.COM:9000", ok: true},
		{host: "pinned.example.com:8443", ok: true},
		{host: "PINNED.example.com:8443", ok: true},
		{host: "pinned.example.com:8080", ok: false},
		{host: "pinned.example.com", ok: false},
		{host: "evil.example.com", ok: false},
		{host: "evil.example.com:8080", ok: false},
		{host: "localhost.evil.com", ok: false},
		{host: "", ok: false},
		{host: "::1", ok: false},
		{host: "[::1", ok: false},
		{host: "[::1]x", ok: false},
		{host: "[127.0.0.1]", ok: false},
		{host: "[localhost]", ok: false},
		{host: "localhost:abc", ok: false},
	}
	h := httpserver.AllowHosts("api.example.com", "Pinned.Example.com:8443", "", "bad:")(okHandler())
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if tt.ok {
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
				}
				return
			}
			assertEnvelope(t, rec, http.StatusMisdirectedRequest, "misdirected_request")
		})
	}
}

func TestAllowOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		ok      bool
	}{
		{name: "no Origin passes", origins: nil, ok: true},
		{name: "listed origin", origins: []string{"https://app.example.com"}, ok: true},
		{name: "listed origin with port", origins: []string{"http://localhost:3000"}, ok: true},
		{name: "scheme and host compare case-insensitively", origins: []string{"HTTPS://App.Example.com"}, ok: true},
		{name: "unlisted origin", origins: []string{"https://evil.example.com"}, ok: false},
		{name: "other scheme", origins: []string{"http://app.example.com"}, ok: false},
		{name: "other port", origins: []string{"http://localhost:3001"}, ok: false},
		{name: "explicit default port is a different origin", origins: []string{"https://app.example.com:443"}, ok: false},
		{name: "null is never allowed", origins: []string{"null"}, ok: false},
		{name: "null is never allowed in any case", origins: []string{"NULL"}, ok: false},
		{name: "several Origin headers", origins: []string{"https://app.example.com", "https://app.example.com"}, ok: false},
	}
	h := httpserver.AllowOrigins("https://app.example.com", "http://localhost:3000", "null", "Null")(okHandler())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, o := range tt.origins {
				req.Header.Add("Origin", o)
			}
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			for k := range rec.Header() {
				if strings.HasPrefix(k, "Access-Control-") {
					t.Errorf("response carries the CORS header %s", k)
				}
			}
			if tt.ok {
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
				}
				return
			}
			assertEnvelope(t, rec, http.StatusForbidden, "forbidden_origin")
		})
	}
}

func TestLimitBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "body within the limit is read whole", body: "12345", ok: true},
		{name: "body over the limit fails with MaxBytesError", body: "123456", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				got     []byte
				readErr error
			)
			h := httpserver.LimitBody(5)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got, readErr = io.ReadAll(r.Body)
			}))

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body)))

			if tt.ok {
				if readErr != nil || string(got) != tt.body {
					t.Errorf("ReadAll() = %q, %v, want %q, nil", got, readErr, tt.body)
				}
				return
			}
			var maxErr *http.MaxBytesError
			if !errors.As(readErr, &maxErr) || maxErr.Limit != 5 {
				t.Errorf("ReadAll() error = %v, want *http.MaxBytesError with limit 5", readErr)
			}
		})
	}
}

func TestLimitBodyOversizeJSONIsAnswered413(t *testing.T) {
	var decodeErr error
	h := httpserver.LimitBody(8)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var v map[string]string
		decodeErr = httpserver.DecodeJSON(r, &v)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"too long for the limit"}`))
	req.Header.Set("Content-Type", "application/json")

	h.ServeHTTP(httptest.NewRecorder(), req)

	var reqErr *httpserver.RequestError
	if !errors.As(decodeErr, &reqErr) || reqErr.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("DecodeJSON() = %v, want a 413 *RequestError", decodeErr)
	}
}

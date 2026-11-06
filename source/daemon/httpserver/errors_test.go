package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.vipengele.org/daemon/httpserver"
)

func TestErrorWritesTheEnvelopeAndHeaders(t *testing.T) {
	rec := httptest.NewRecorder()

	httpserver.Error(rec, http.StatusNotFound, "not_found", "no such job")

	want := "{\n  \"schema\": 1,\n  \"error\": {\n    \"code\": \"not_found\",\n    \"message\": \"no such job\"\n  }\n}\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	for k, v := range map[string]string{
		"Content-Type":           "application/json",
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
	} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

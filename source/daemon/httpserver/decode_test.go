package httpserver_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.vipengele.org/daemon/httpserver"
)

type payload struct {
	Name string `json:"name"`
}

func decode(t *testing.T, contentType, body string, limit int64) (payload, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if limit > 0 {
		req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, limit)
	}
	var p payload
	err := httpserver.DecodeJSON(req, &p)
	return p, err
}

func TestDecodeJSONAcceptsJSONContentTypes(t *testing.T) {
	for _, ct := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"APPLICATION/JSON",
		"application/merge-patch+json",
		"application/vnd.api+json; charset=utf-8",
	} {
		p, err := decode(t, ct, `{"name":"x"}`, 0)
		if err != nil || p.Name != "x" {
			t.Errorf("%q: got %+v, %v", ct, p, err)
		}
	}
}

func TestDecodeJSONRejects(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		limit       int64
		status      int
		code        string
	}{
		{"missing content type", "", `{}`, 0, 415, "unsupported_media_type"},
		{"wrong content type", "text/plain", `{}`, 0, 415, "unsupported_media_type"},
		{"malformed content type", "application/json;;=", `{}`, 0, 415, "unsupported_media_type"},
		{"unknown field", "application/json", `{"other":1}`, 0, 400, "bad_request"},
		{"trailing data", "application/json", `{"name":"x"} {}`, 0, 400, "bad_request"},
		{"trailing garbage", "application/json", `{"name":"x"} x`, 0, 400, "bad_request"},
		{"malformed json", "application/json", `{"name":`, 0, 400, "bad_request"},
		{"empty body", "application/json", ``, 0, 400, "bad_request"},
		{"limit reached by trailing data", "application/json", `{"name":"x"}` + strings.Repeat(" ", 8) + `{}`, 16, 413, "body_too_large"},
		{"oversize body", "application/json", `{"name":"` + strings.Repeat("a", 100) + `"}`, 16, 413, "body_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decode(t, tt.contentType, tt.body, tt.limit)
			var re *httpserver.RequestError
			if !errors.As(err, &re) {
				t.Fatalf("err = %v, want *RequestError", err)
			}
			if re.Status != tt.status || re.Code != tt.code || re.Message == "" {
				t.Errorf("got %d %s %q, want %d %s", re.Status, re.Code, re.Message, tt.status, tt.code)
			}
		})
	}
}

func TestRequestErrorTextIsCodeAndMessage(t *testing.T) {
	err := &httpserver.RequestError{Status: http.StatusBadRequest, Code: "bad_request", Message: "body is malformed"}

	if got, want := err.Error(), "bad_request: body is malformed"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

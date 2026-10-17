package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactorLineText(t *testing.T) {
	r := newRedactor(Redaction{Prefixes: []string{"ghp_", "sk-"}, Headers: []string{"X-Api-Key"}})
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tool prefix", "token ghp_abc123/+=.~-_ end", "token ghp_[REDACTED] end"},
		{"second tool prefix", "key=sk-XYZ", "key=sk-[REDACTED]"},
		{"bare prefix untouched", "the ghp_ prefix", "the ghp_ prefix"},
		{"prefix at end untouched", "ends with ghp_", "ends with ghp_"},
		{"bearer", "sent Bearer abc.def-ghi now", "sent Bearer [REDACTED] now"},
		{"basic", "sent Basic dXNlcjpwYXNz= now", "sent Basic [REDACTED] now"},
		{"bare bearer untouched", "a Bearer  token", "a Bearer  token"},
		{"baseline header", "Authorization: Bearer abc", "Authorization: [REDACTED]"},
		{"header case-insensitive", "authorization: secret value", "authorization: [REDACTED]"},
		{"proxy header", "Proxy-Authorization: Basic xyz", "Proxy-Authorization: [REDACTED]"},
		{"cookie", "Cookie: a=b; c=d", "Cookie: [REDACTED]"},
		{"set-cookie", "SET-COOKIE: sid=1; Path=/", "SET-COOKIE: [REDACTED]"},
		{"tool header", "x-api-key: 12345", "x-api-key: [REDACTED]"},
		{"header value ends at line end", "Cookie: a=b\nnext line", "Cookie: [REDACTED]\nnext line"},
		{"nothing to redact", "plain text", "plain text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.line(tt.in, redactText); got != tt.want {
				t.Errorf("line(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactorLineJSON(t *testing.T) {
	r := newRedactor(Redaction{Prefixes: []string{"ghp_"}})
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"header in a string ends at the closing quote",
			`{"msg":"Authorization: Bearer abc","n":1}`,
			`{"msg":"Authorization: [REDACTED]","n":1}`,
		},
		{
			"escaped quote does not end the value",
			`{"msg":"Cookie: a=\"b\" c","n":1}`,
			`{"msg":"Cookie: [REDACTED]","n":1}`,
		},
		{
			"header key",
			`{"AUTHORIZATION":"secret","n":1}`,
			`{"AUTHORIZATION":"[REDACTED]","n":1}`,
		},
		{
			"prefix",
			`{"msg":"use ghp_abc","n":1}`,
			`{"msg":"use ghp_[REDACTED]","n":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.line(tt.in, redactJSON)
			if got != tt.want {
				t.Errorf("line(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("line(%q) = %q, not valid JSON", tt.in, got)
			}
		})
	}
}

func TestRedactorZeroRedactionKeepsBaseline(t *testing.T) {
	r := newRedactor(Redaction{})
	for in, want := range map[string]string{
		"Authorization: x":       "Authorization: [REDACTED]",
		"Proxy-Authorization: x": "Proxy-Authorization: [REDACTED]",
		"Cookie: x":              "Cookie: [REDACTED]",
		"Set-Cookie: x":          "Set-Cookie: [REDACTED]",
		"Bearer abc":             "Bearer [REDACTED]",
		"Basic abc":              "Basic [REDACTED]",
	} {
		if got := r.line(in, redactText); got != want {
			t.Errorf("line(%q) = %q, want %q", in, got, want)
		}
	}
	for _, key := range []string{"authorization", "Proxy-Authorization", "COOKIE", "set-cookie"} {
		if !r.isHeader(key) {
			t.Errorf("isHeader(%q) = false, want true", key)
		}
	}
}

// stringer formats as a token-bearing string.
type stringer struct{}

func (stringer) String() string { return "via ghp_stringer" }

// valuer resolves to a group holding a token.
type valuer struct{}

func (valuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("inner", "ghp_valuer"), slog.String("cookie", "c=1"))
}

// plain is a struct whose %+v form carries a token.
type plain struct{ Token string }

// logJSON logs through a redacting handler over a JSON handler and returns the decoded record.
func logJSON(t *testing.T, red Redaction, use func(*slog.Logger)) (string, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	h := newRedactingHandler(slog.NewJSONHandler(&buf, nil), newRedactor(red))
	use(slog.New(h))
	out := buf.String()
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("output %q is not one JSON record: %v", out, err)
	}
	return out, rec
}

func assertNoSecret(t *testing.T, out string) {
	t.Helper()
	for _, secret := range []string{"abc", "ghp_valuer", "ghp_stringer", "ghp_err", "ghp_plain", "c=1", "s3cret"} {
		if strings.Contains(out, secret) {
			t.Errorf("output %q leaks %q", out, secret)
		}
	}
}

func TestHandlerRedactsMessage(t *testing.T) {
	out, rec := logJSON(t, Redaction{Prefixes: []string{"ghp_"}}, func(l *slog.Logger) {
		l.Info("calling with Bearer abc and ghp_abc")
	})
	assertNoSecret(t, out)
	if got, want := rec["msg"], "calling with Bearer [REDACTED] and ghp_[REDACTED]"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
}

func TestHandlerRedactsAttributes(t *testing.T) {
	out, rec := logJSON(t, Redaction{Prefixes: []string{"ghp_"}}, func(l *slog.Logger) {
		l.Info("m",
			slog.String("token", "ghp_abc"),
			slog.String("AUTHORIZATION", "s3cret"),
			slog.Int("count", 3),
			slog.String("note", "bare ghp_ stays"),
		)
	})
	assertNoSecret(t, out)
	for key, want := range map[string]any{
		"token":         "ghp_[REDACTED]",
		"AUTHORIZATION": "[REDACTED]",
		"count":         float64(3),
		"note":          "bare ghp_ stays",
	} {
		if got := rec[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

func TestHandlerRedactsFormattedValues(t *testing.T) {
	out, rec := logJSON(t, Redaction{Prefixes: []string{"ghp_"}}, func(l *slog.Logger) {
		l.Info("m",
			slog.Any("err", errors.New("failed with ghp_err")),
			slog.Any("str", stringer{}),
			slog.Any("val", valuer{}),
			slog.Any("plain", plain{Token: "ghp_plain"}),
			slog.Any("clean", plain{Token: "nothing"}),
		)
	})
	assertNoSecret(t, out)
	if got, want := rec["err"], "failed with ghp_[REDACTED]"; got != want {
		t.Errorf("err = %v, want %v", got, want)
	}
	if got, want := rec["str"], "via ghp_[REDACTED]"; got != want {
		t.Errorf("str = %v, want %v", got, want)
	}
	val, ok := rec["val"].(map[string]any)
	if !ok {
		t.Fatalf("val = %v, want a group", rec["val"])
	}
	if got, want := val["inner"], "ghp_[REDACTED]"; got != want {
		t.Errorf("val.inner = %v, want %v", got, want)
	}
	if got, want := val["cookie"], "[REDACTED]"; got != want {
		t.Errorf("val.cookie = %v, want %v", got, want)
	}
	if got, want := rec["plain"], "{Token:ghp_[REDACTED]}"; got != want {
		t.Errorf("plain = %v, want %v", got, want)
	}
	clean, ok := rec["clean"].(map[string]any)
	if !ok || clean["Token"] != "nothing" {
		t.Errorf("clean = %v, want the value unchanged", rec["clean"])
	}
}

func TestHandlerRedactsGroupedAttributes(t *testing.T) {
	out, rec := logJSON(t, Redaction{Prefixes: []string{"ghp_"}}, func(l *slog.Logger) {
		l.Info("m", slog.Group("req",
			slog.Group("headers", slog.String("Set-Cookie", "abc"), slog.String("Accept", "*/*")),
			slog.String("url", "https://x/?t=ghp_abc"),
		))
	})
	assertNoSecret(t, out)
	req, _ := rec["req"].(map[string]any)
	headers, _ := req["headers"].(map[string]any)
	if got, want := headers["Set-Cookie"], "[REDACTED]"; got != want {
		t.Errorf("req.headers.Set-Cookie = %v, want %v", got, want)
	}
	if got, want := headers["Accept"], "*/*"; got != want {
		t.Errorf("req.headers.Accept = %v, want %v", got, want)
	}
	if got, want := req["url"], "https://x/?t=ghp_[REDACTED]"; got != want {
		t.Errorf("req.url = %v, want %v", got, want)
	}
}

func TestHandlerWithAttrsAndWithGroup(t *testing.T) {
	out, rec := logJSON(t, Redaction{Prefixes: []string{"ghp_"}}, func(l *slog.Logger) {
		l.With(slog.String("early", "ghp_abc"), slog.String("cookie", "abc")).
			WithGroup("g").
			With(slog.String("mid", "Bearer abc")).
			Info("m", slog.String("late", "ghp_abc"))
	})
	assertNoSecret(t, out)
	if got, want := rec["early"], "ghp_[REDACTED]"; got != want {
		t.Errorf("early = %v, want %v", got, want)
	}
	if got, want := rec["cookie"], "[REDACTED]"; got != want {
		t.Errorf("cookie = %v, want %v", got, want)
	}
	g, ok := rec["g"].(map[string]any)
	if !ok {
		t.Fatalf("g = %v, want a group", rec["g"])
	}
	if got, want := g["mid"], "Bearer [REDACTED]"; got != want {
		t.Errorf("g.mid = %v, want %v", got, want)
	}
	if got, want := g["late"], "ghp_[REDACTED]"; got != want {
		t.Errorf("g.late = %v, want %v", got, want)
	}
}

func TestHandlerEnabledDelegates(t *testing.T) {
	next := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := newRedactingHandler(next, newRedactor(Redaction{}))
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(info) = true under a warn handler, want false")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(error) = false under a warn handler, want true")
	}
}

func TestHandlerTextOutput(t *testing.T) {
	var buf bytes.Buffer
	h := newRedactingHandler(slog.NewTextHandler(&buf, nil), newRedactor(Redaction{}))
	slog.New(h).Info("m", slog.String("Cookie", "abc"), slog.String("auth", "Basic abc"))
	out := buf.String()
	assertNoSecret(t, out)
	if !strings.Contains(out, "Cookie=[REDACTED]") || !strings.Contains(out, `auth="Basic [REDACTED]"`) {
		t.Errorf("output %q does not carry the redacted attributes", out)
	}
}

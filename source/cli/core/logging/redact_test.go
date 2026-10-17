package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
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
			if got := r.lineAs(tt.in, shapeText); got != tt.want {
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
			got := r.lineAs(tt.in, shapeJSON)
			if got != tt.want {
				t.Errorf("line(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("line(%q) = %q, not valid JSON", tt.in, got)
			}
		})
	}
}

func TestNonEmptyDropsEmptyAndDuplicates(t *testing.T) {
	got := nonEmpty([]string{"b", "", "a", "b", "", "a"})
	if len(got) != 2 || !slices.Contains(got, "a") || !slices.Contains(got, "b") {
		t.Errorf("nonEmpty = %q, want a and b once each", got)
	}
}

// TestRedactorPrefixExtendingAnother pins that a prefix that extends another is matched whole,
// whatever order the prefixes are given in: the run after the shorter prefix would otherwise keep
// the rest of the longer one in front of "[REDACTED]".
func TestRedactorPrefixExtendingAnother(t *testing.T) {
	for _, prefixes := range [][]string{{"ab_", "ab_long_"}, {"ab_long_", "ab_"}} {
		r := newRedactor(Redaction{Prefixes: prefixes})
		for in, want := range map[string]string{
			"x ab_long_XYZ y": "x ab_long_[REDACTED] y",
			"x ab_XYZ y":      "x ab_[REDACTED] y",
		} {
			if got := r.lineAs(in, shapeText); got != want {
				t.Errorf("prefixes %q: line(%q) = %q, want %q", prefixes, in, got, want)
			}
		}
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
		if got := r.lineAs(in, shapeText); got != want {
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
	h := newRedactingHandler(slog.NewJSONHandler(&buf, nil), newRedactor(red), spec.FormatJSON)
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
	// The JSON handler encodes a Stringer as JSON, not by its String method.
	if str, ok := rec["str"].(map[string]any); !ok || len(str) != 0 {
		t.Errorf("str = %v, want an empty object", rec["str"])
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
	plainRec, ok := rec["plain"].(map[string]any)
	if !ok || plainRec["Token"] != "ghp_[REDACTED]" {
		t.Errorf("plain = %v, want an object with Token ghp_[REDACTED]", rec["plain"])
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
	h := newRedactingHandler(next, newRedactor(Redaction{}), spec.FormatText)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(info) = true under a warn handler, want false")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(error) = false under a warn handler, want true")
	}
}

func TestHandlerTextOutput(t *testing.T) {
	var buf bytes.Buffer
	h := newRedactingHandler(slog.NewTextHandler(&buf, nil), newRedactor(Redaction{}), spec.FormatText)
	slog.New(h).Info("m", slog.String("Cookie", "abc"), slog.String("auth", "Basic abc"))
	out := buf.String()
	assertNoSecret(t, out)
	if !strings.Contains(out, "Cookie=[REDACTED]") || !strings.Contains(out, `auth="Basic [REDACTED]"`) {
		t.Errorf("output %q does not carry the redacted attributes", out)
	}
}

func TestRedactorLineByShape(t *testing.T) {
	r := newRedactor(Redaction{Prefixes: []string{"ghp_"}, Headers: []string{"X-Api-Key"}})
	tests := []struct {
		name, in, want string
	}{
		{"plain quoted cookie", `Set-Cookie: sid="abc123"; Path=/`, "Set-Cookie: [REDACTED]"},
		{"json cookie key", `{"Cookie":"sid=abc"}`, `{"Cookie":"[REDACTED]"}`},
		{"json tool header key", `{"X-Api-Key":"k1","n":1}`, `{"X-Api-Key":"[REDACTED]","n":1}`},
		{"json with spaces", `  {"x-api-key" : "k1"} `, `  {"x-api-key" : "[REDACTED]"} `},
		{"json header in a string", `{"msg":"Cookie: a=\"b\" c","n":1}`, `{"msg":"Cookie: [REDACTED]","n":1}`},
		{"invalid json is text", `{"Cookie":"cut`, `{"Cookie":[REDACTED]`},
		{"quoted key in text", `plain "X-Api-Key":"k2" tail`, `plain "X-Api-Key":[REDACTED]`},
		{"json array", `[{"Cookie":"a"}]`, `[{"Cookie":"[REDACTED]"}]`},
		{"bracketed text", `[INFO] Cookie: a="b" c`, `[INFO] Cookie: [REDACTED]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.line(tt.in)
			if got != tt.want {
				t.Errorf("line(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if again := r.line(got); again != got {
				t.Errorf("line(%q) = %q, want it unchanged", got, again)
			}
			if json.Valid([]byte(tt.in)) && !json.Valid([]byte(got)) {
				t.Errorf("line(%q) = %q, not valid JSON", tt.in, got)
			}
		})
	}
}

// headers nests a header map behind a pointer, which %+v prints as an address and the JSON
// encoder follows.
type headers struct {
	In *struct{ H map[string][]string }
}

// textToken writes a token through MarshalText that its %+v form does not show.
type textToken struct{ id int }

func (textToken) MarshalText() ([]byte, error) { return []byte("ghp_text"), nil }

// failingText fails MarshalText with a token in the error.
type failingText struct{}

func (failingText) MarshalText() ([]byte, error) { return nil, errors.New("bad ghp_fail") }

// nilErr panics in Error on a nil receiver.
type nilErr struct{ msg string }

func (e *nilErr) Error() string { return e.msg }

func TestHandlerJSONRedactsTheEncodedForm(t *testing.T) {
	out, rec := logJSON(t, Redaction{Prefixes: []string{"ghp_"}}, func(l *slog.Logger) {
		l.Info("m",
			slog.Any("req", headers{In: &struct{ H map[string][]string }{H: map[string][]string{"Cookie": {"abc"}}}}),
			slog.Any("text", textToken{}),
			slog.Any("fail", failingText{}),
		)
	})
	assertNoSecret(t, out)
	for _, s := range []string{"ghp_text", "ghp_fail"} {
		if strings.Contains(out, s) {
			t.Errorf("output %q leaks %q", out, s)
		}
	}
	req, _ := rec["req"].(map[string]any)
	in, _ := req["In"].(map[string]any)
	h, _ := in["H"].(map[string]any)
	if h["Cookie"] != "[REDACTED]" {
		t.Errorf("req = %v, want In.H.Cookie redacted", rec["req"])
	}
	if got, want := rec["text"], "ghp_[REDACTED]"; got != want {
		t.Errorf("text = %v, want %v", got, want)
	}
}

// namedBytes is a type whose underlying type is a byte slice, which the text handler writes as
// the quoted bytes.
type namedBytes []byte

func TestHandlerTextRedactsTheWrittenForm(t *testing.T) {
	var buf bytes.Buffer
	h := newRedactingHandler(slog.NewTextHandler(&buf, nil), newRedactor(Redaction{Prefixes: []string{"ghp_"}}), spec.FormatText)
	slog.New(h).Info("m",
		slog.Any("text", textToken{}),
		slog.Any("bytes", []byte("Bearer abc")),
		slog.Any("named", namedBytes("Bearer abc")),
		slog.Any("plain", plain{Token: "ghp_plain"}),
		slog.Any("fail", failingText{}),
	)
	out := buf.String()
	for _, s := range []string{"ghp_text", "abc", "ghp_plain", "ghp_fail"} {
		if strings.Contains(out, s) {
			t.Errorf("output %q leaks %q", out, s)
		}
	}
	for _, want := range []string{
		"text=ghp_[REDACTED]", `bytes="Bearer [REDACTED]"`, `named="Bearer [REDACTED]"`,
		"plain={Token:ghp_[REDACTED]}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q lacks %q", out, want)
		}
	}
}

// TestHandlerJSONHandsOnAnInvalidRedactionAsAString pins that a value whose redacted encoding is
// not valid JSON, here a prefix matched inside a number, is written as a string, so the record
// stays valid JSON and carries the redacted text.
func TestHandlerJSONHandsOnAnInvalidRedactionAsAString(t *testing.T) {
	_, rec := logJSON(t, Redaction{Prefixes: []string{"1."}}, func(l *slog.Logger) {
		l.Info("m", slog.Any("v", map[string]float64{"n": 1.5}))
	})
	if got, want := rec["v"], `{"n":1.[REDACTED]}`; got != want {
		t.Errorf("v = %v, want %q", got, want)
	}
}

// TestHandlerKeepsCleanValuesAsWritten pins that a value with no secret is written by the
// redacting handler exactly as the handler beneath it writes it alone.
func TestHandlerKeepsCleanValuesAsWritten(t *testing.T) {
	noTime := &slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey && len(groups) == 0 {
			return slog.Attr{}
		}
		return a
	}}
	attrs := []any{
		slog.Any("struct", plain{Token: "x <&> y"}),
		slog.Any("ptr", &plain{Token: "p"}),
		slog.Any("map", map[string]any{"b": []int{1, 2}, "a": nil}),
		slog.Any("err", errors.New("plain failure")),
		slog.Any("nilerr", (*nilErr)(nil)),
		slog.Any("str", stringer{}),
		slog.Any("bytes", []byte("raw bytes")),
		slog.Any("raw", json.RawMessage(`{"k": [1, 2]}`)),
		slog.Any("nil", nil),
		slog.Any("text", textToken{id: 1}),
	}
	for _, tt := range []struct {
		format spec.Format
		next   func(*bytes.Buffer) slog.Handler
	}{
		{spec.FormatJSON, func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, noTime) }},
		{spec.FormatText, func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, noTime) }},
	} {
		t.Run(tt.format.String(), func(t *testing.T) {
			var plainOut, redactedOut bytes.Buffer
			slog.New(tt.next(&plainOut)).Info("m", attrs...)
			r := newRedactor(Redaction{Prefixes: []string{"zzz_"}})
			slog.New(newRedactingHandler(tt.next(&redactedOut), r, tt.format)).Info("m", attrs...)
			if got, want := redactedOut.String(), plainOut.String(); got != want {
				t.Errorf("redacting handler wrote\n%s\nwant\n%s", got, want)
			}
		})
	}
}

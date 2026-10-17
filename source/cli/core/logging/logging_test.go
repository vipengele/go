package logging_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"go.vipengele.org/cli/core/logging"
	"go.vipengele.org/cli/spec"
)

func restoreDefault(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"DEBUG", slog.LevelDebug, false},
		{"Warn", slog.LevelWarn, false},
		{"", 0, true},
		{"trace", 0, true},
		{"warning", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := logging.ParseLevel(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseLevel(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err != nil {
				if got != 0 {
					t.Errorf("ParseLevel(%q) = %v on error, want the zero level", tt.in, got)
				}
				for _, name := range []string{"debug", "info", "warn", "error"} {
					if !strings.Contains(err.Error(), name) {
						t.Errorf("error %q does not name %q", err, name)
					}
				}
				return
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSetupWritesAtLevel(t *testing.T) {
	tests := []struct {
		name   string
		format spec.Format
		level  slog.Level
		want   []string
		absent []string
	}{
		{"text info", spec.FormatText, slog.LevelInfo, []string{"level=INFO", "msg=shown", "k=v"}, []string{"hidden"}},
		{"text debug", spec.FormatText, slog.LevelDebug, []string{"level=DEBUG", "msg=hidden"}, nil},
		{"json info", spec.FormatJSON, slog.LevelInfo, []string{`"level":"INFO"`, `"msg":"shown"`, `"k":"v"`}, []string{"hidden"}},
		{"json error", spec.FormatJSON, slog.LevelError, []string{`"level":"ERROR"`}, []string{"shown", "hidden"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreDefault(t)
			var buf bytes.Buffer
			res, err := logging.Setup(logging.Options{Level: tt.level, Format: tt.format, Writer: &buf})
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			logger := res.Logger
			logger.Debug("hidden")
			logger.Info("shown", "k", "v")
			logger.Error("failed")
			out := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("output %q lacks %q", out, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(out, a) {
					t.Errorf("output %q contains %q", out, a)
				}
			}
		})
	}
}

func TestSetupInstallsDefault(t *testing.T) {
	restoreDefault(t)
	var buf bytes.Buffer
	res, err := logging.Setup(logging.Options{Writer: &buf})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if slog.Default() != res.Logger {
		t.Error("slog.Default() is not the returned logger")
	}
	slog.Info("via default")
	if !strings.Contains(buf.String(), "msg=\"via default\"") {
		t.Errorf("default logger did not write to the buffer: %q", buf.String())
	}
}

func TestSetupRejectsUnsupportedFormat(t *testing.T) {
	restoreDefault(t)
	before := slog.Default()
	res, err := logging.Setup(logging.Options{Format: spec.Format(99), Writer: io.Discard})
	if err == nil {
		t.Fatal("Setup accepted an unsupported format")
	}
	if res.Logger != nil || res.Writer != nil {
		t.Errorf("result = %+v, want the zero Result", res)
	}
	if slog.Default() != before {
		t.Error("default logger changed on error")
	}
}

func TestSetupNilWriterUsesStderr(t *testing.T) {
	restoreDefault(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	res, err := logging.Setup(logging.Options{})
	if err != nil {
		os.Stderr = orig
		t.Fatalf("Setup: %v", err)
	}
	res.Logger.Info("to stderr")
	os.Stderr = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "to stderr") {
		t.Errorf("stderr received %q", out)
	}
}

// baselineSecret is a secret of the baseline every [logging.Redaction] redacts, with the header
// name or prefix that stays in the output.
type baselineSecret struct{ key, value, kept string }

// baselineSecrets returns a secret of each kind the baseline redacts.
func baselineSecrets() []baselineSecret {
	return []baselineSecret{
		{"Authorization", "s3cret-authorization", ""},
		{"Proxy-Authorization", "s3cret-proxy", ""},
		{"Cookie", "s3cret-cookie", ""},
		{"Set-Cookie", "s3cret-set-cookie", ""},
		{"token", "Bearer s3cret-bearer", "Bearer "},
		{"basic", "Basic s3cret-basic", "Basic "},
	}
}

func formats() []spec.Format { return []spec.Format{spec.FormatText, spec.FormatJSON} }

// checkRedacted fails t when out holds a secret value or, in JSON, a line that is not valid
// JSON.
func checkRedacted(t *testing.T, format spec.Format, out string, secrets ...string) {
	t.Helper()
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("output %q holds no [REDACTED]", out)
	}
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("output %q leaks %q", out, s)
		}
	}
	if format != spec.FormatJSON {
		return
	}
	for line := range strings.Lines(out) {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %q is not valid JSON", line)
		}
	}
}

// secretPart returns the part of value a redactor removes: all of it for a header, the token
// after kept for a prefix.
func secretPart(value, kept string) string { return strings.TrimPrefix(value, kept) }

func TestSetupRedactsBaselineThroughHandler(t *testing.T) {
	for _, format := range formats() {
		for _, sec := range baselineSecrets() {
			t.Run(format.String()+"/"+sec.key, func(t *testing.T) {
				restoreDefault(t)
				var buf bytes.Buffer
				res, err := logging.Setup(logging.Options{Format: format, Writer: &buf})
				if err != nil {
					t.Fatalf("Setup: %v", err)
				}
				res.Logger.Info("request", sec.key, sec.value)
				res.Logger.Info("sent " + sec.key + ": " + sec.value)
				res.Logger.With(sec.key, sec.value).WithGroup("g").Info("grouped", sec.key, sec.value)
				out := buf.String()
				checkRedacted(t, format, out, secretPart(sec.value, sec.kept))
				if !strings.Contains(out, sec.key) {
					t.Errorf("output %q lost the key %q", out, sec.key)
				}
			})
		}
	}
}

func TestSetupRedactsBaselineThroughWriter(t *testing.T) {
	for _, format := range formats() {
		for _, sec := range baselineSecrets() {
			t.Run(format.String()+"/"+sec.key, func(t *testing.T) {
				restoreDefault(t)
				var buf bytes.Buffer
				res, err := logging.Setup(logging.Options{Format: format, Writer: &buf})
				if err != nil {
					t.Fatalf("Setup: %v", err)
				}
				var lines []string
				if format == spec.FormatJSON {
					lines = []string{
						`{"msg":"raw","` + sec.key + `":"` + sec.value + `"}` + "\n",
						`{"msg":"` + sec.key + `: ` + sec.value + `"}`,
					}
				} else {
					lines = []string{
						"raw " + sec.key + ": " + sec.value + "\n",
						"tail " + sec.key + ": " + sec.value,
					}
				}
				for _, l := range lines {
					if _, err := io.WriteString(res.Writer, l); err != nil {
						t.Fatalf("Write: %v", err)
					}
				}
				if err := res.Writer.Flush(); err != nil {
					t.Fatalf("Flush: %v", err)
				}
				checkRedacted(t, format, buf.String(), secretPart(sec.value, sec.kept))
			})
		}
	}
}

func TestSetupRedactsToolAdditions(t *testing.T) {
	red := logging.Redaction{Prefixes: []string{"ghp_"}, Headers: []string{"X-Api-Key"}}
	for _, format := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			restoreDefault(t)
			var buf bytes.Buffer
			res, err := logging.Setup(logging.Options{Format: format, Writer: &buf, Redaction: red})
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			res.Logger.Info("pushed with ghp_handlerToken1", "x-api-key", "handler-key", "auth", "Bearer handler-bearer")
			line := "raw ghp_writerToken2 X-Api-Key: writer-key\n"
			if format == spec.FormatJSON {
				line = `{"msg":"raw ghp_writerToken2","X-Api-Key":"writer-key"}` + "\n"
			}
			if _, err := io.WriteString(res.Writer, line); err != nil {
				t.Fatalf("Write: %v", err)
			}
			out := buf.String()
			checkRedacted(t, format, out,
				"handlerToken1", "handler-key", "handler-bearer", "writerToken2", "writer-key")
			if !strings.Contains(out, "ghp_[REDACTED]") {
				t.Errorf("output %q does not keep the prefix before [REDACTED]", out)
			}
		})
	}
}

// failingWriter fails every write with err.
type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestSetupWriterReportsDestinationError(t *testing.T) {
	restoreDefault(t)
	errWrite := errors.New("write failed")
	res, err := logging.Setup(logging.Options{Writer: failingWriter{err: errWrite}})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := io.WriteString(res.Writer, "line\n"); !errors.Is(err, errWrite) {
		t.Errorf("Write error = %v, want %v", err, errWrite)
	}
}

// TestSetupWriterReadsLinesByShape pins that the writer reads a raw line by its own shape, whatever
// the log format: a plain line under JSON keeps no part of a quoted cookie value, and a JSON line
// under text has its header keys redacted and stays valid JSON.
func TestSetupWriterReadsLinesByShape(t *testing.T) {
	red := logging.Redaction{Headers: []string{"X-Api-Key"}}
	tests := []struct {
		name, in, want string
	}{
		{"plain quoted cookie", `Set-Cookie: sid="abc123"; Path=/` + "\n", "Set-Cookie: [REDACTED]\n"},
		{"json cookie key", `{"Cookie":"sid=abc"}` + "\n", `{"Cookie":"[REDACTED]"}` + "\n"},
		{"json tool header key", `{"X-Api-Key":"k-123","n":1}` + "\n", `{"X-Api-Key":"[REDACTED]","n":1}` + "\n"},
		{"json fragment", `cut {"Cookie":"sid=abc"` + "\n", `cut {"Cookie":[REDACTED]` + "\n"},
	}
	for _, format := range formats() {
		for _, tt := range tests {
			t.Run(format.String()+"/"+tt.name, func(t *testing.T) {
				restoreDefault(t)
				var buf bytes.Buffer
				res, err := logging.Setup(logging.Options{Format: format, Writer: &buf, Redaction: red})
				if err != nil {
					t.Fatalf("Setup: %v", err)
				}
				if _, err := io.WriteString(res.Writer, tt.in); err != nil {
					t.Fatalf("Write: %v", err)
				}
				if got := buf.String(); got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			})
		}
	}
}

// TestSetupJSONLinesStayValidThroughBothLayers pins that a JSON line stays valid JSON after the
// handler and then the writer redact it, and that a raw JSON line written beside the records stays
// valid in either format.
func TestSetupJSONLinesStayValidThroughBothLayers(t *testing.T) {
	red := logging.Redaction{Prefixes: []string{"ghp_"}, Headers: []string{"X-Api-Key"}}
	raw := `{"msg":"Authorization: Bearer abc \"q\" end","Cookie":"a=\"b\"","x":"ghp_tok"}` + "\n"
	for _, format := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			restoreDefault(t)
			var buf bytes.Buffer
			res, err := logging.Setup(logging.Options{Format: format, Writer: &buf, Redaction: red})
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			if format == spec.FormatJSON {
				res.Logger.Info(`Set-Cookie: sid="abc"; Path=/`, "Cookie", "abc", "body", `{"X-Api-Key":"abc"}`,
					"note", "Authorization: Bearer abc", "tok", "ghp_abc")
			}
			if _, err := io.WriteString(res.Writer, raw); err != nil {
				t.Fatalf("Write: %v", err)
			}
			for line := range strings.Lines(buf.String()) {
				line = strings.TrimSuffix(line, "\n")
				if !json.Valid([]byte(line)) {
					t.Errorf("line %q is not valid JSON", line)
				}
				for _, secret := range []string{"abc", `\"b`, "ghp_tok"} {
					if strings.Contains(line, secret) {
						t.Errorf("line %q leaks %q", line, secret)
					}
				}
			}
		})
	}
}

// headerHolder and request nest an http.Header behind two pointers, which %+v prints as an
// address and the JSON encoder follows.
type headerHolder struct{ Header http.Header }

type request struct{ In *headerHolder }

func TestSetupRedactsHeaderBehindNestedPointers(t *testing.T) {
	for _, format := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			restoreDefault(t)
			var buf bytes.Buffer
			res, err := logging.Setup(logging.Options{Format: format, Writer: &buf})
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			req := &request{In: &headerHolder{Header: http.Header{
				"Cookie": {"sid=abc"}, "Set-Cookie": {"a=1", "b=2"}, "Accept": {"*/*"},
			}}}
			res.Logger.Info("m", slog.Any("req", req), slog.Any("map", map[string]any{"Cookie": 9876543210}))
			out := buf.String()
			for _, secret := range []string{"sid=abc", "a=1", "b=2", "9876543210"} {
				if strings.Contains(out, secret) {
					t.Errorf("output %q leaks %q", out, secret)
				}
			}
			if format != spec.FormatJSON {
				return
			}
			line := strings.TrimSuffix(out, "\n")
			if !json.Valid([]byte(line)) {
				t.Fatalf("line %q is not valid JSON", line)
			}
			var rec struct {
				Req struct {
					In struct{ Header map[string]any }
				}
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("decode %q: %v", line, err)
			}
			h := rec.Req.In.Header
			if h["Cookie"] != "[REDACTED]" || h["Set-Cookie"] != "[REDACTED]" {
				t.Errorf("header = %v, want Cookie and Set-Cookie redacted", h)
			}
			if accept, _ := h["Accept"].([]any); len(accept) != 1 || accept[0] != "*/*" {
				t.Errorf("header Accept = %v, want it kept as an array", h["Accept"])
			}
		})
	}
}

// dumpBytes is a named byte-slice type, which the JSON handler writes in base64 as it does a []byte.
type dumpBytes []byte

// TestSetupJSONRedactsByteSlices pins that under the JSON format a secret in the bytes of a byte
// slice, a []byte or a named byte-slice type, at the top of an attribute or inside an object, is
// redacted and written as text, neither in clear nor in the base64 form the JSON handler gives
// bytes, and the record stays one JSON object.
func TestSetupJSONRedactsByteSlices(t *testing.T) {
	restoreDefault(t)
	var buf bytes.Buffer
	res, err := logging.Setup(logging.Options{Format: spec.FormatJSON, Writer: &buf})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	dump := "GET / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer tok\r\n\r\n"
	res.Logger.Info("m",
		slog.Any("dump", []byte(dump)),
		slog.Any("named", dumpBytes("Authorization: Bearer tok")),
		slog.Any("req", struct{ Body []byte }{Body: []byte("Cookie: sid=tok")}),
	)
	out := buf.String()
	for _, s := range []string{
		"tok", base64.StdEncoding.EncodeToString([]byte(dump)),
		base64.StdEncoding.EncodeToString([]byte("Authorization: Bearer tok")),
		base64.StdEncoding.EncodeToString([]byte("Cookie: sid=tok")),
	} {
		if strings.Contains(out, s) {
			t.Errorf("output %q leaks %q", out, s)
		}
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("output %q is not one JSON record: %v", out, err)
	}
	// Read as JSON, the header value runs to the closing quote of the string.
	if got, want := rec["dump"], "GET / HTTP/1.1\r\nHost: x\r\nAuthorization: [REDACTED]"; got != want {
		t.Errorf("dump = %q, want %q", got, want)
	}
	if got, want := rec["named"], "Authorization: [REDACTED]"; got != want {
		t.Errorf("named = %q, want %q", got, want)
	}
	req, _ := rec["req"].(map[string]any)
	if got, want := req["Body"], "Cookie: [REDACTED]"; got != want {
		t.Errorf("req.Body = %q, want %q", got, want)
	}
}

// bodyHolder holds a byte slice, which %+v writes as its decimal byte values.
type bodyHolder struct{ Body []byte }

// bodyNode links to itself through a pointer, which %+v prints as an address below the top.
type bodyNode struct {
	Next *bodyNode
	Body []byte
}

// TestSetupTextRedactsNestedByteSlices pins that under the text format a secret in the bytes of a
// byte slice or byte array inside a slog.Any value, which %+v writes as decimal byte values, is
// written redacted as a quoted string, neither in clear nor as its decimal form, while clean bytes
// beside it keep their decimal form.
func TestSetupTextRedactsNestedByteSlices(t *testing.T) {
	secret := []byte("Bearer tok")
	cyclic := &bodyNode{Body: secret}
	cyclic.Next = cyclic
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"struct field", bodyHolder{Body: secret}, `v="{Body:\"Bearer [REDACTED]\"}"`},
		{"map value", map[string][]byte{"a": secret, "b": []byte("ok")}, `v="map[a:\"Bearer [REDACTED]\" b:[111 107]]"`},
		{"slice of structs", []bodyHolder{{Body: []byte("x")}, {Body: secret}}, `v="[{Body:[120]} {Body:\"Bearer [REDACTED]\"}]"`},
		{"pointer", &bodyHolder{Body: secret}, `v="&{Body:\"Bearer [REDACTED]\"}"`},
		{"named type", struct{ D dumpBytes }{D: dumpBytes(secret)}, `v="{D:\"Bearer [REDACTED]\"}"`},
		{"interface field", struct{ X any }{X: secret}, `v="{X:\"Bearer [REDACTED]\"}"`},
		{"unexported field", struct{ body []byte }{body: secret}, `v="{body:\"Bearer [REDACTED]\"}"`},
		{"array", struct{ A [10]byte }{A: [10]byte(secret)}, `v="{A:\"Bearer [REDACTED]\"}"`},
		{"unexported array", struct{ a [10]byte }{a: [10]byte(secret)}, `v="{a:\"Bearer [REDACTED]\"}"`},
		{"array in an interface", struct{ X any }{X: [10]byte(secret)}, `v="{X:\"Bearer [REDACTED]\"}"`},
		{"map key", map[[10]byte]int{[10]byte(secret): 1}, `v="map[\"Bearer [REDACTED]\":1]"`},
		{"cyclic pointer", cyclic, `Body:\"Bearer [REDACTED]\"}"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreDefault(t)
			var buf bytes.Buffer
			res, err := logging.Setup(logging.Options{Format: spec.FormatText, Writer: &buf})
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			res.Logger.Info("m", slog.Any("v", tt.value))
			out := buf.String()
			for _, s := range []string{"tok", strings.Trim(fmt.Sprintf("%d", secret), "[]")} {
				if strings.Contains(out, s) {
					t.Errorf("output %q leaks %q", out, s)
				}
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("output %q lacks %q", out, tt.want)
			}
		})
	}
}

// TestSetupTextRedactsNestedHeaderBytes pins that a header in the bytes of a nested byte slice
// has its value redacted under the text format, through both layers.
func TestSetupTextRedactsNestedHeaderBytes(t *testing.T) {
	restoreDefault(t)
	var buf bytes.Buffer
	res, err := logging.Setup(logging.Options{Format: spec.FormatText, Writer: &buf})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	body := []byte("Cookie: sid=tok")
	res.Logger.Info("m", slog.Any("req", bodyHolder{Body: body}))
	out := buf.String()
	for _, s := range []string{"sid=tok", strings.Trim(fmt.Sprintf("%d", body), "[]")} {
		if strings.Contains(out, s) {
			t.Errorf("output %q leaks %q", out, s)
		}
	}
	if want := `req="{Body:\"Cookie: [REDACTED]`; !strings.Contains(out, want) {
		t.Errorf("output %q lacks %q", out, want)
	}
}

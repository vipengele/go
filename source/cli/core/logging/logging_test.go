package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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

// baselineSecrets are secrets of the baseline every [logging.Redaction] redacts, each with the
// header name or prefix that stays in the output.
var baselineSecrets = []struct { //nolint:gochecknoglobals // read-only test table
	key, value, kept string
}{
	{"Authorization", "s3cret-authorization", ""},
	{"Proxy-Authorization", "s3cret-proxy", ""},
	{"Cookie", "s3cret-cookie", ""},
	{"Set-Cookie", "s3cret-set-cookie", ""},
	{"token", "Bearer s3cret-bearer", "Bearer "},
	{"basic", "Basic s3cret-basic", "Basic "},
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
		for _, sec := range baselineSecrets {
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
		for _, sec := range baselineSecrets {
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

// failingWriter fails every write.
type failingWriter struct{}

var errWrite = errors.New("write failed") //nolint:gochecknoglobals // sentinel

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestSetupWriterReportsDestinationError(t *testing.T) {
	restoreDefault(t)
	res, err := logging.Setup(logging.Options{Writer: failingWriter{}})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := io.WriteString(res.Writer, "line\n"); !errors.Is(err, errWrite) {
		t.Errorf("Write error = %v, want %v", err, errWrite)
	}
}

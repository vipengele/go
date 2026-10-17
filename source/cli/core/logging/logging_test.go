package logging_test

import (
	"bytes"
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
			logger, err := logging.Setup(logging.Options{Level: tt.level, Format: tt.format, Writer: &buf})
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
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
	logger, err := logging.Setup(logging.Options{Writer: &buf})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if slog.Default() != logger {
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
	logger, err := logging.Setup(logging.Options{Format: spec.Format(99), Writer: io.Discard})
	if err == nil {
		t.Fatal("Setup accepted an unsupported format")
	}
	if logger != nil {
		t.Errorf("logger = %v, want nil", logger)
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

	logger, err := logging.Setup(logging.Options{})
	if err != nil {
		os.Stderr = orig
		t.Fatalf("Setup: %v", err)
	}
	logger.Info("to stderr")
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

package config_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.vipengele.org/cli/core/command/config"
)

func TestValidateAcceptsAValidFile(t *testing.T) {
	opts := options(t, "name: tool\nport: 9090\nserver:\n  host: example.com\n")
	r := run(t, context.Background(), opts, "validate", false)
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if want := opts.Path + " is valid"; !strings.Contains(r.stderr, want) {
		t.Errorf("stderr = %q, want a line holding %q", r.stderr, want)
	}
}

func TestValidateAcceptsAMissingFile(t *testing.T) {
	opts := options(t, "")
	r := run(t, context.Background(), opts, "validate", false)
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if want := opts.Path + " does not exist"; !strings.Contains(r.stderr, want) {
		t.Errorf("stderr = %q, want a line holding %q", r.stderr, want)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			"file problems",
			"name: tool\nbogus: 1\nserver:\n  hots: x\nport:\n",
			[]string{"bogus", "server.hots", "config.yaml:2", "config.yaml:4", "config.yaml:5"},
		},
		{
			"values that do not convert",
			"port: abc\nretries: many\n",
			[]string{`port: invalid int "abc"`, `retries: invalid int "many"`},
		},
		{
			"validator problems",
			"name: \"\"\nport: 70000\n",
			[]string{"name: must not be empty", "port: must be below 65536", "config.yaml:1", "config.yaml:2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, context.Background(), options(t, tt.content), "validate", false)
			if r.err == nil {
				t.Fatal("Run succeeded, want the problems of the file")
			}
			for _, want := range tt.want {
				if !strings.Contains(r.err.Error(), want) {
					t.Errorf("error %q does not mention %q", r.err, want)
				}
			}
			if r.stdout != "" || r.stderr != "" {
				t.Errorf("stdout %q, stderr %q; want both empty", r.stdout, r.stderr)
			}
		})
	}
}

func TestValidateReportsAFileThatIsNotYAML(t *testing.T) {
	opts := options(t, "port: [unclosed\n")
	r := run(t, context.Background(), opts, "validate", false)
	if r.err == nil {
		t.Fatal("Run succeeded, want the parse error")
	}
	if !strings.Contains(r.err.Error(), opts.Path) {
		t.Errorf("error %q does not name the file %q", r.err, opts.Path)
	}
}

func TestValidateIgnoresTheEnvironment(t *testing.T) {
	opts := options(t, "")
	opts.LookupEnv = func(key string) (string, bool) { return "not a number", key == "TOOL_PORT" }
	if r := run(t, context.Background(), opts, "validate", false); r.err != nil {
		t.Errorf("Run: %v, want the file judged without the environment", r.err)
	}
}

func TestValidateCreatesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	opts := config.Options[testConfig]{Path: filepath.Join(dir, "config.yaml"), Defaults: defaults()}
	if r := run(t, context.Background(), opts, "validate", false); r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat of the config directory = %v, want it not to exist", err)
	}
}

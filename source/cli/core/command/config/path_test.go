package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.vipengele.org/cli/core/command/config"
	"go.vipengele.org/cli/spec/speccheck"
)

func TestPathPrintsThePath(t *testing.T) {
	opts := options(t, "")
	r := run(t, context.Background(), opts, "path", false)
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if want := opts.Path + "\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
}

func TestPathJSONReportsWhetherTheFileExists(t *testing.T) {
	tests := []struct {
		name    string
		content string
		exists  bool
	}{
		{"missing", "", false},
		{"present", "port: 80\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := options(t, tt.content)
			r := run(t, context.Background(), opts, "path", true)
			if r.err != nil {
				t.Fatalf("Run: %v", r.err)
			}
			var got struct {
				Schema int    `json:"schema"`
				Path   string `json:"path"`
				Exists bool   `json:"exists"`
			}
			if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
				t.Fatalf("stdout %q is not JSON: %v", r.stdout, err)
			}
			if got.Schema != 1 || got.Path != opts.Path || got.Exists != tt.exists {
				t.Errorf("JSON = %+v, want schema 1, path %q, exists %t", got, opts.Path, tt.exists)
			}
			var keys map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &keys); err != nil {
				t.Fatalf("stdout is not an object: %v", err)
			}
			speccheck.AssertJSONKeys(t, keys, filepath.Join("testdata", "path-json.golden"))
		})
	}
}

func TestPathCreatesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	opts := config.Options[testConfig]{Path: filepath.Join(dir, "config.yaml"), Defaults: defaults()}
	for _, asJSON := range []bool{false, true} {
		if r := run(t, context.Background(), opts, "path", asJSON); r.err != nil {
			t.Fatalf("Run with JSON %t: %v", asJSON, r.err)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat of the config directory = %v, want it not to exist", err)
	}
}

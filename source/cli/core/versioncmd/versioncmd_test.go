package versioncmd_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/core/versioncmd"
)

func run(t *testing.T, info buildinfo.Info, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := versioncmd.New(info)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestText(t *testing.T) {
	tests := []struct {
		name string
		info buildinfo.Info
		want string
	}{
		{"version and commit", buildinfo.Info{Version: "v1.0.0", Commit: "0123abcdef0123abcdef"}, "v1.0.0 build 0123abcdef01\n"},
		{"version only", buildinfo.Info{Version: "v1.0.0"}, "v1.0.0\n"},
		{"unknown version", buildinfo.Info{}, "dev\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := run(t, tt.info)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJSON(t *testing.T) {
	tests := []struct {
		name    string
		info    buildinfo.Info
		version string
		dev     bool
	}{
		{"release", buildinfo.Info{Version: "v1.0.0", Commit: "0123abcdef0123abcdef", Date: "2026-01-02T03:04:05Z", GoVersion: "go1.27.2", OS: "linux", Arch: "amd64"}, "v1.0.0", false},
		{"unknown version", buildinfo.Info{}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := run(t, tt.info, "--json")
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !strings.HasSuffix(got, "}\n") || strings.Count(got, "\n") != 1 {
				t.Fatalf("output = %q, want one object and a newline", got)
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(got), &m); err != nil {
				t.Fatalf("output is not JSON: %v", err)
			}
			for _, k := range []string{"schema", "version", "commit", "date", "go", "os", "arch", "dev"} {
				if _, ok := m[k]; !ok {
					t.Errorf("key %q missing from %q", k, got)
				}
			}
			if m["schema"] != float64(1) {
				t.Errorf("schema = %v, want 1", m["schema"])
			}
			if m["version"] != tt.version {
				t.Errorf("version = %v, want %q", m["version"], tt.version)
			}
			if m["dev"] != tt.dev {
				t.Errorf("dev = %v, want %v", m["dev"], tt.dev)
			}
		})
	}
}

func TestRejectsArguments(t *testing.T) {
	if _, err := run(t, buildinfo.Info{Version: "v1.0.0"}, "extra"); err == nil {
		t.Fatal("Execute returned nil, want an error for a positional argument")
	}
}

func TestJSONIsLocalFlag(t *testing.T) {
	cmd := versioncmd.New(buildinfo.Info{})
	if cmd.Flags().Lookup("json") == nil {
		t.Fatal("--json is not declared on the command")
	}
	if cmd.PersistentFlags().Lookup("json") != nil {
		t.Error("--json is persistent, want local")
	}
}

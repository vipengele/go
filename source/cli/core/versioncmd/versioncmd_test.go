package versioncmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/core/versioncmd"
	"go.vipengele.org/cli/spec"
)

func root(info buildinfo.Info) *spec.Command {
	return &spec.Command{
		Name:     "tool",
		Commands: []*spec.Command{versioncmd.New(info)},
	}
}

func run(t *testing.T, info buildinfo.Info, args ...string) (stdout string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := &spec.Env{Stdout: &out, Stderr: &errOut}
	code = cli.ExecuteArgs(env, root(info), args)
	return out.String(), code
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
			got, code := run(t, tt.info, "version")
			if code != cli.ExitOK {
				t.Fatalf("exit code = %d, want %d", code, cli.ExitOK)
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
			got, code := run(t, tt.info, "version", "--json")
			if code != cli.ExitOK {
				t.Fatalf("exit code = %d, want %d", code, cli.ExitOK)
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
	got, code := run(t, buildinfo.Info{Version: "v1.0.0"}, "version", "extra")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}

func TestJSONIsLocalFlag(t *testing.T) {
	cmd := versioncmd.New(buildinfo.Info{})
	if len(cmd.Flags) != 1 {
		t.Fatalf("flags = %d, want 1", len(cmd.Flags))
	}
	f := cmd.Flags[0]
	if f.Name() != "json" || f.Kind() != spec.BoolKind {
		t.Errorf("flag = --%s of kind %v, want --json of kind %v", f.Name(), f.Kind(), spec.BoolKind)
	}
	if f.Inherited() {
		t.Error("--json is inherited, want local")
	}

	if _, code := run(t, buildinfo.Info{}, "--json"); code != cli.ExitUsage {
		t.Errorf("exit code of the root with --json = %d, want %d", code, cli.ExitUsage)
	}
}

func TestRunWritesToEnvStdout(t *testing.T) {
	var out bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdout: &out}, nil, nil)
	if err := versioncmd.New(buildinfo.Info{Version: "v1.0.0"}).Run(context.Background(), inv); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := out.String(), "v1.0.0\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

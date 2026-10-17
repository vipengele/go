package version_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/core/command/version"
	"go.vipengele.org/cli/spec"
)

func root(info buildinfo.Info) *spec.Command {
	return &spec.Command{
		Name:     "tool",
		Commands: []*spec.Command{version.New(info)},
	}
}

func run(t *testing.T, info buildinfo.Info, args ...string) (stdout string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := &spec.Env{Stdout: &out, Stderr: &errOut}
	code = cli.ExecuteArgs(env, root(info), args)
	return out.String(), code
}

// versionJSON returns what "version --json" writes for info.
func versionJSON(t *testing.T, info buildinfo.Info) string {
	t.Helper()
	var out bytes.Buffer
	if err := spec.WriteJSON(&out, 1, info); err != nil {
		t.Fatalf("spec.WriteJSON: %v", err)
	}
	return out.String()
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
			if !strings.HasPrefix(got, "{\n  \"schema\": 1,\n") || !strings.HasSuffix(got, "\n}\n") {
				t.Fatalf("output = %q, want an indented object led by schema and a newline", got)
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

func TestJSONIsLocalFlagByDefault(t *testing.T) {
	cmd := version.New(buildinfo.Info{})
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

	if got, code := run(t, buildinfo.Info{Version: "v1.0.0"}, "version", "--json"); code != cli.ExitOK || !strings.HasPrefix(got, "{") {
		t.Errorf("version --json = %d, %q; want 0 and JSON", code, got)
	}
	if _, code := run(t, buildinfo.Info{}, "--json"); code != cli.ExitUsage {
		t.Errorf("exit code of the root with --json = %d, want %d", code, cli.ExitUsage)
	}
}

func TestWithOutputJSONReadsTheOutput(t *testing.T) {
	info := buildinfo.Info{Version: "v1.0.0"}
	cmd := version.New(info, version.WithOutputJSON())
	if len(cmd.Flags) != 0 {
		t.Fatalf("flags = %d, want none", len(cmd.Flags))
	}
	want := versionJSON(t, info)
	for _, asJSON := range []bool{false, true} {
		var out bytes.Buffer
		inv := spec.NewInvocation(&spec.Env{Stdout: &out}, nil, nil)
		inv.Output = spec.NewOutput(spec.OutputOptions{JSON: asJSON})
		if err := cmd.Run(context.Background(), inv); err != nil {
			t.Fatalf("Run with Output.JSON %t: %v", asJSON, err)
		}
		wantOut := "v1.0.0\n"
		if asJSON {
			wantOut = want
		}
		if got := out.String(); got != wantOut {
			t.Errorf("output with Output.JSON %t = %q, want %q", asJSON, got, wantOut)
		}
	}

	for _, args := range [][]string{{"version", "--json"}, {"--json", "version"}} {
		var out, errOut bytes.Buffer
		app := cli.New(spec.Identity{Name: "tool"}, cli.WithJSON(), cli.WithBuildInfo(info))
		code := app.ExecuteArgs(&spec.Env{Stdout: &out, Stderr: &errOut}, args)
		if code != cli.ExitOK || out.String() != want {
			t.Errorf("%q = %d, stdout %q, stderr %q; want 0 and %q", args, code, out.String(), errOut.String(), want)
		}
	}
}

func TestLocalJSONUnderAnInheritedJSONIsInvalid(t *testing.T) {
	inherited := spec.BoolFlag("json", "write JSON", spec.Inherited())
	tests := []struct {
		name string
		cmd  *spec.Command
		ok   bool
	}{
		{"local flag", version.New(buildinfo.Info{}), false},
		{"output JSON", version.New(buildinfo.Info{}, version.WithOutputJSON()), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := &spec.Command{Name: "tool", Flags: []spec.Flag{inherited}, Commands: []*spec.Command{tt.cmd}}
			if err := root.Validate(); (err == nil) != tt.ok {
				t.Errorf("Validate = %v, want valid %t", err, tt.ok)
			}
		})
	}
}

func TestRunWritesToEnvStdout(t *testing.T) {
	var out bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdout: &out}, nil, nil)
	if err := version.New(buildinfo.Info{Version: "v1.0.0"}).Run(context.Background(), inv); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := out.String(), "v1.0.0\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestLocalJSONReadsTheOutputFormatToo(t *testing.T) {
	info := buildinfo.Info{Version: "v1.0.0"}
	want := versionJSON(t, info)
	tests := []struct {
		name   string
		values map[string]any
		format spec.Format
		want   string
	}{
		{"neither", nil, spec.FormatText, "v1.0.0\n"},
		{"local --json", map[string]any{"json": true}, spec.FormatText, want},
		{"JSON format", nil, spec.FormatJSON, want},
		{"both", map[string]any{"json": true}, spec.FormatJSON, want},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			inv := spec.NewInvocation(&spec.Env{Stdout: &out}, nil, tt.values)
			inv.Output = spec.NewOutput(spec.OutputOptions{Format: tt.format})
			if err := version.New(info).Run(context.Background(), inv); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := out.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionUnderAnAppWithOutput(t *testing.T) {
	info := buildinfo.Info{Version: "v1.0.0"}
	want := versionJSON(t, info)
	tests := []struct {
		args       []string
		wantCode   int
		wantStdout string
	}{
		{[]string{"version"}, cli.ExitOK, "v1.0.0\n"},
		{[]string{"version", "--output", "text"}, cli.ExitOK, "v1.0.0\n"},
		{[]string{"version", "--json"}, cli.ExitOK, want},
		{[]string{"version", "--output", "json"}, cli.ExitOK, want},
		{[]string{"--output=json", "version"}, cli.ExitOK, want},
		{[]string{"version", "--json", "--output", "json"}, cli.ExitOK, want},
		{[]string{"version", "--json", "--output", "text"}, cli.ExitUsage, ""},
		{[]string{"version", "--json=false", "--output", "json"}, cli.ExitUsage, ""},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		app := cli.New(spec.Identity{Name: "tool"}, cli.WithOutput(), cli.WithBuildInfo(info))
		code := app.ExecuteArgs(&spec.Env{Stdout: &out, Stderr: &errOut}, tt.args)
		if code != tt.wantCode || out.String() != tt.wantStdout {
			t.Errorf("%q = %d, stdout %q, stderr %q; want %d and %q",
				tt.args, code, out.String(), errOut.String(), tt.wantCode, tt.wantStdout)
		}
	}
}

package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"go.vipengele.org/cli/core/command/config"
	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/speccheck"
)

// loaded returns the Options of a file setting port, and a context carrying the configuration
// of defaults under that file under TOOL_RETRIES=5.
func loaded(t *testing.T) (config.Options[testConfig], context.Context) {
	t.Helper()
	opts := options(t, "name: tool\nport: 9090\n")
	opts.LookupEnv = func(key string) (string, bool) {
		if key == "TOOL_RETRIES" {
			return "5", true
		}
		return "", false
	}
	res, err := opts.Load(spec.NewInvocation(nil, nil, nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return opts, config.With(context.Background(), res)
}

func TestShowTextNamesEveryOrigin(t *testing.T) {
	opts, ctx := loaded(t)
	r := run(t, ctx, opts, "show", false)
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	want := strings.Join([]string{
		"KEY          VALUE      ORIGIN",
		"name         tool       config.yaml:1",
		"port         9090       config.yaml:2",
		"retries      5          env TOOL_RETRIES",
		"server.host  localhost  default",
		"",
	}, "\n")
	if r.stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", r.stdout, want)
	}
}

func TestShowTextFollowsTheStdoutColour(t *testing.T) {
	opts, ctx := loaded(t)
	var out bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdout: &out}, nil, nil)
	if err := subcommand(t, config.New(opts), "show").Run(ctx, inv); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := spec.Styler.Bold(inv.Output.Styler(spec.Stdout), "KEY"); !strings.HasPrefix(out.String(), want) {
		t.Errorf("stdout = %q, want it to start with the bold header %q", out.String(), want)
	}
}

func TestShowJSONNamesEveryOrigin(t *testing.T) {
	opts, ctx := loaded(t)
	r := run(t, ctx, opts, "show", true)
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if !strings.HasPrefix(r.stdout, "{\n  \"schema\": 1,\n") {
		t.Errorf("stdout = %q, want an envelope led by schema 1", r.stdout)
	}
	var got struct {
		Path   string `json:"path"`
		Values []struct {
			Key    string `json:"key"`
			Value  string `json:"value"`
			Origin string `json:"origin"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", r.stdout, err)
	}
	if got.Path != opts.Path {
		t.Errorf("path = %q, want %q", got.Path, opts.Path)
	}
	origins := map[string]string{}
	for _, v := range got.Values {
		origins[v.Key] = v.Value + " from " + v.Origin
	}
	want := map[string]string{
		"name":        "tool from config.yaml:1",
		"port":        "9090 from config.yaml:2",
		"retries":     "5 from env TOOL_RETRIES",
		"server.host": "localhost from default",
	}
	for key, w := range want {
		if origins[key] != w {
			t.Errorf("%s = %q, want %q", key, origins[key], w)
		}
	}
	if len(got.Values) != len(want) {
		t.Errorf("values = %d, want %d", len(got.Values), len(want))
	}

	var keys map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &keys); err != nil {
		t.Fatalf("stdout is not an object: %v", err)
	}
	speccheck.AssertJSONKeys(t, keys, filepath.Join("testdata", "show-json.golden"))
}

func TestShowWithoutALoadedConfigurationFails(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"bare context", context.Background()},
		{"another type", config.With(context.Background(), mustLoadOther(t))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, tt.ctx, options(t, ""), "show", false)
			if !errors.Is(r.err, config.ErrNotLoaded) {
				t.Errorf("Run = %v, want an error matching ErrNotLoaded", r.err)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
		})
	}
}

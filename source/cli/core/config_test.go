package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "go.vipengele.org/config"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// toolConfig is the configuration the App of these tests loads: keys "name", "port" and
// "retries".
type toolConfig struct {
	Name    string
	Port    int
	Retries int
}

// Validate requires a port below 65536.
func (c *toolConfig) Validate() error {
	if c.Port > 65535 {
		return &cfg.FieldError{Key: "port", Err: errors.New("must be below 65536")}
	}
	return nil
}

func toolDefaults() toolConfig {
	return toolConfig{Name: "tool", Port: 8080, Retries: 3}
}

// configEnv is the environment of one App execution: TOOL_HOME is home, so the configuration
// file is home/config/config.yaml, and vars are the other variables set.
type configEnv struct {
	home string
	vars map[string]string
}

// newConfigEnv returns a configEnv under a new home directory, its configuration file holding
// content unless content is "", in which case no file is written.
func newConfigEnv(t *testing.T, content string) configEnv {
	t.Helper()
	e := configEnv{home: t.TempDir(), vars: map[string]string{}}
	if content != "" {
		if err := os.MkdirAll(filepath.Dir(e.path()), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(e.path(), []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return e
}

// path is the configuration file of e.
func (e configEnv) path() string {
	return filepath.Join(e.home, "config", "config.yaml")
}

// run executes app with args against e, standard input empty and no stream a terminal.
func (e configEnv) run(t *testing.T, app *cli.App, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := &spec.Env{
		Stdout:  &stdout,
		Stderr:  &stderr,
		WorkDir: t.TempDir(),
		LookupEnv: func(key string) (string, bool) {
			if key == "TOOL_HOME" {
				return e.home, true
			}
			v, ok := e.vars[key]
			return v, ok
		},
		Terminal: func(spec.Stream) bool { return false },
	}
	code := app.ExecuteArgs(env, args)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// seen is what a run function found in its context.
type seen struct {
	called bool
	ok     bool
	value  toolConfig
}

// readsConfig returns a command named name, annotated by annotations, that records in s what
// ConfigFrom returns.
func readsConfig(name string, annotations map[string]string, s *seen) *spec.Command {
	return &spec.Command{
		Name:        name,
		Summary:     "Read the configuration",
		Annotations: annotations,
		Run: func(ctx context.Context, _ *spec.Invocation) error {
			r, ok := cli.ConfigFrom[toolConfig](ctx)
			*s = seen{called: true, ok: ok, value: r.Value()}
			return nil
		},
	}
}

// configApp returns an App with WithConfig and the extra opts, holding readsConfig as "read"
// and, annotated SkipConfig, as "skip".
func configApp(s *seen, opts ...cli.Option) *cli.App {
	app := cli.New(identity(), append(opts, cli.WithBuildInfo(versionInfo()), cli.WithConfig(toolDefaults()))...)
	app.Add(readsConfig("read", nil, s))
	app.Add(readsConfig("skip", map[string]string{spec.SkipConfig: ""}, s))
	return app
}

func TestWithConfigLoadsEveryLayer(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "port: 9090\nretries: 4\n")
	e.vars["TOOL_RETRIES"] = "5"
	if got := e.run(t, configApp(&s), "read"); got != (result{code: cli.ExitOK}) {
		t.Fatalf("ExecuteArgs = %+v, want success and no output", got)
	}
	want := seen{called: true, ok: true, value: toolConfig{Name: "tool", Port: 9090, Retries: 5}}
	if s != want {
		t.Errorf("run saw %+v, want %+v", s, want)
	}
}

func TestWithConfigMissingFileLoadsTheDefaults(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "")
	if got := e.run(t, configApp(&s), "read"); got != (result{code: cli.ExitOK}) {
		t.Fatalf("ExecuteArgs = %+v, want success and no output", got)
	}
	if want := (seen{called: true, ok: true, value: toolDefaults()}); s != want {
		t.Errorf("run saw %+v, want %+v", s, want)
	}
	if _, err := os.Stat(filepath.Dir(e.path())); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config directory: Stat = %v, want it not created", err)
	}
}

func TestWithConfigLoadFailureStopsTheRun(t *testing.T) {
	tests := []struct {
		name    string
		content string
		vars    map[string]string
		want    []string
	}{
		{
			name:    "file",
			content: "port: [1]\n",
			want:    []string{"config.yaml:1", "port"},
		},
		{
			name:    "validator",
			content: "port: 70000\n",
			want:    []string{"port", "must be below 65536"},
		},
		{
			name: "environment",
			vars: map[string]string{"TOOL_PORT": "many"},
			want: []string{"TOOL_PORT", "many"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s seen
			e := newConfigEnv(t, tt.content)
			for k, v := range tt.vars {
				e.vars[k] = v
			}
			got := e.run(t, configApp(&s), "read")
			if got.code != cli.ExitError {
				t.Errorf("exit code = %d, want %d", got.code, cli.ExitError)
			}
			if s.called {
				t.Error("the run function was called")
			}
			if got.stdout != "" {
				t.Errorf("stdout = %q, want empty", got.stdout)
			}
			if !strings.HasPrefix(got.stderr, "tool: ") {
				t.Errorf("stderr = %q, want it led by the tool's name", got.stderr)
			}
			for _, w := range tt.want {
				if !strings.Contains(got.stderr, w) {
					t.Errorf("stderr = %q, want it to contain %q", got.stderr, w)
				}
			}
		})
	}
}

func TestWithConfigBrokenFileRendersEveryProblemOnItsOwnRow(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "name: [x]\nport: [1]\nretries: {a: 1}\n")
	got := e.run(t, configApp(&s), "read")
	if got.code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d; stderr %q", got.code, cli.ExitError, got.stderr)
	}
	lines := strings.Split(strings.TrimSuffix(got.stderr, "\n"), "\n")
	want := []string{
		"tool: load config",
		`  • config.yaml:1: key "name" `,
		`  • config.yaml:2: key "port" `,
		`  • config.yaml:3: key "retries" `,
	}
	if len(lines) != len(want) || lines[0] != want[0] {
		t.Fatalf("stderr =\n%s\nwant the line %q and one row per problem", got.stderr, want[0])
	}
	for i, w := range want[1:] {
		if !strings.HasPrefix(lines[i+1], w) {
			t.Errorf("row %d = %q, want it to start with %q", i+1, lines[i+1], w)
		}
	}
}

func TestWithConfigSkippedCommandRunsWithoutConfiguration(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "port: [1]\n")
	if got := e.run(t, configApp(&s), "skip"); got != (result{code: cli.ExitOK}) {
		t.Fatalf("ExecuteArgs = %+v, want success and no output", got)
	}
	if want := (seen{called: true}); s != want {
		t.Errorf("run saw %+v, want %+v", s, want)
	}
}

func TestWithConfigVersionRunsWithABrokenFile(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "port: [1]\n")
	got := e.run(t, configApp(&s), "version")
	if got.code != cli.ExitOK || got.stderr != "" {
		t.Fatalf("ExecuteArgs = %+v, want success and no stderr", got)
	}
	if !strings.Contains(got.stdout, versionInfo().Version) {
		t.Errorf("stdout = %q, want the version", got.stdout)
	}
}

func TestWithConfigRunsAheadOfEveryPreRunHook(t *testing.T) {
	var s seen
	var hook seen
	user := cli.WithPreRun(func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
		r, ok := cli.ConfigFrom[toolConfig](ctx)
		hook = seen{called: true, ok: ok, value: r.Value()}
		return ctx, nil
	})
	e := newConfigEnv(t, "port: 9090\n")
	if got := e.run(t, configApp(&s, user), "read"); got != (result{code: cli.ExitOK}) {
		t.Fatalf("ExecuteArgs = %+v, want success and no output", got)
	}
	want := seen{called: true, ok: true, value: toolConfig{Name: "tool", Port: 9090, Retries: 3}}
	if hook != want {
		t.Errorf("pre-run hook added before WithConfig saw %+v, want %+v", hook, want)
	}
	if s != want {
		t.Errorf("run saw %+v, want %+v", s, want)
	}
}

func TestWithConfigFailureStopsEveryPreRunHook(t *testing.T) {
	var s seen
	var called bool
	user := cli.WithPreRun(func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
		called = true
		return ctx, nil
	})
	e := newConfigEnv(t, "port: [1]\n")
	if got := e.run(t, configApp(&s, user), "read"); got.code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d", got.code, cli.ExitError)
	}
	if called || s.called {
		t.Errorf("pre-run hook called %v, run called %v; want neither", called, s.called)
	}
}

func TestWithoutConfigThereIsNoConfiguration(t *testing.T) {
	var s seen
	app := cli.New(identity())
	app.Add(readsConfig("read", nil, &s))
	e := newConfigEnv(t, "port: [1]\n")
	if got := e.run(t, app, "read"); got != (result{code: cli.ExitOK}) {
		t.Fatalf("ExecuteArgs = %+v, want success and no output", got)
	}
	if want := (seen{called: true}); s != want {
		t.Errorf("run saw %+v, want %+v", s, want)
	}
	got := e.run(t, app, "config", "path")
	if got.code != cli.ExitUsage || !strings.Contains(got.stderr, `unknown command "config"`) {
		t.Errorf("config path = %+v, want an unknown command", got)
	}
}

func TestWithConfigToolCommandNamedConfigIsADuplicate(t *testing.T) {
	var s seen
	app := configApp(&s)
	app.Add(readsConfig("config", nil, &s))
	got := newConfigEnv(t, "").run(t, app, "read")
	if got.code != cli.ExitError || !strings.Contains(got.stderr, "invalid command tree") {
		t.Errorf("ExecuteArgs = %+v, want an invalid command tree", got)
	}
}

func TestWithConfigUnresolvableDirectory(t *testing.T) {
	var s seen
	var stderr bytes.Buffer
	env := &spec.Env{Stderr: &stderr, WorkDir: t.TempDir()}
	// An identity whose DirName starts with '.' resolves no directory on any platform.
	bad := identity()
	bad.DirName = ".tool"
	app := cli.New(bad, cli.WithConfig(toolDefaults()))
	app.Add(readsConfig("read", nil, &s))
	app.Add(readsConfig("skip", map[string]string{spec.SkipConfig: ""}, &s))
	if code := app.ExecuteArgs(env, []string{"read"}); code != cli.ExitError {
		t.Errorf("read: exit code = %d, want %d; stderr %q", code, cli.ExitError, stderr.String())
	}
	if s.called {
		t.Error("read: the run function was called")
	}
	if !strings.HasPrefix(stderr.String(), "tool: config\n  → paths: ") {
		t.Errorf("read: stderr = %q, want the configuration directory's error", stderr.String())
	}
	stderr.Reset()
	if code := app.ExecuteArgs(env, []string{"skip"}); code != cli.ExitOK || !s.called {
		t.Errorf("skip: exit code = %d, called %v; want success; stderr %q", code, s.called, stderr.String())
	}
	stderr.Reset()
	if code := app.ExecuteArgs(env, []string{"config", "path"}); code != cli.ExitError ||
		!strings.HasPrefix(stderr.String(), "tool: config\n  → paths: ") {
		t.Errorf("config path: exit code = %d, stderr %q; want the configuration directory's error", code, stderr.String())
	}
}

func TestConfigPath(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "")
	if got, want := e.run(t, configApp(&s), "config", "path"), (result{code: cli.ExitOK, stdout: e.path() + "\n"}); got != want {
		t.Errorf("config path = %+v, want %+v", got, want)
	}
	got := e.run(t, configApp(&s, cli.WithJSON()), "config", "path", "--json")
	if got.code != cli.ExitOK {
		t.Fatalf("config path --json = %+v, want success", got)
	}
	var doc struct {
		Schema int    `json:"schema"`
		Path   string `json:"path"`
		Exists bool   `json:"exists"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", got.stdout, err)
	}
	if doc.Schema != 1 || doc.Path != e.path() || doc.Exists {
		t.Errorf("config path --json = %+v, want schema 1, path %q and no file", doc, e.path())
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		code     int
		inStderr string
	}{
		{name: "valid file", content: "port: 9090\n", code: cli.ExitOK, inStderr: "is valid"},
		{name: "missing file", code: cli.ExitOK, inStderr: "does not exist"},
		{name: "broken file", content: "port: [1]\n", code: cli.ExitError, inStderr: "config.yaml:1"},
		{name: "invalid value", content: "port: 70000\n", code: cli.ExitError, inStderr: "must be below 65536"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s seen
			got := newConfigEnv(t, tt.content).run(t, configApp(&s), "config", "validate")
			if got.code != tt.code || got.stdout != "" || !strings.Contains(got.stderr, tt.inStderr) {
				t.Errorf("config validate = %+v, want code %d, no stdout and %q on stderr", got, tt.code, tt.inStderr)
			}
		})
	}
}

func TestConfigShow(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "port: 9090\n")
	e.vars["TOOL_RETRIES"] = "5"
	got := e.run(t, configApp(&s), "config", "show")
	if got.code != cli.ExitOK || got.stderr != "" {
		t.Fatalf("config show = %+v, want success and no stderr", got)
	}
	for _, row := range [][]string{
		{"name", "tool", "default"},
		{"port", "9090", "config.yaml:1"},
		{"retries", "5", "env TOOL_RETRIES"},
	} {
		if !containsRow(got.stdout, row) {
			t.Errorf("stdout =\n%s\nwant a row %q", got.stdout, row)
		}
	}

	got = e.run(t, configApp(&s, cli.WithJSON()), "config", "show", "--json")
	if got.code != cli.ExitOK || got.stderr != "" {
		t.Fatalf("config show --json = %+v, want success and no stderr", got)
	}
	var doc struct {
		Schema int    `json:"schema"`
		Path   string `json:"path"`
		Values []struct {
			Key    string `json:"key"`
			Value  string `json:"value"`
			Origin string `json:"origin"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", got.stdout, err)
	}
	origins := map[string]string{}
	for _, v := range doc.Values {
		origins[v.Key] = v.Value + " from " + v.Origin
	}
	want := map[string]string{
		"name":    "tool from default",
		"port":    "9090 from config.yaml:1",
		"retries": "5 from env TOOL_RETRIES",
	}
	if doc.Schema != 1 || doc.Path != e.path() || len(origins) != len(want) {
		t.Errorf("config show --json = %+v, want schema 1, path %q and %d values", doc, e.path(), len(want))
	}
	for k, w := range want {
		if origins[k] != w {
			t.Errorf("%s = %q, want %q", k, origins[k], w)
		}
	}
}

// containsRow reports whether a line of table holds the fields of row, in order, separated by
// whitespace alone.
func containsRow(table string, row []string) bool {
	for _, l := range strings.Split(table, "\n") {
		if strings.Join(strings.Fields(l), " ") == strings.Join(row, " ") {
			return true
		}
	}
	return false
}

func TestConfigShowFailsOnABrokenFile(t *testing.T) {
	var s seen
	got := newConfigEnv(t, "port: [1]\n").run(t, configApp(&s), "config", "show")
	if got.code != cli.ExitError || got.stdout != "" || !strings.Contains(got.stderr, "config.yaml:1") {
		t.Errorf("config show = %+v, want exit 1, no stdout and the file's problem on stderr", got)
	}
}

func TestConfigEditRefusesANonTerminal(t *testing.T) {
	var s seen
	e := newConfigEnv(t, "")
	e.vars["EDITOR"] = "true"
	got := e.run(t, configApp(&s), "config", "edit")
	if got.code != cli.ExitError || !strings.Contains(got.stderr, "not a terminal") {
		t.Errorf("config edit = %+v, want exit 1 and the terminal refusal", got)
	}
	if _, err := os.Stat(filepath.Dir(e.path())); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config directory: Stat = %v, want it not created", err)
	}
}

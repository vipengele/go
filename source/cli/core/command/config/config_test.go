package config_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/command/config"
	"go.vipengele.org/cli/spec"
	cfg "go.vipengele.org/config"
)

// server is a nested group of keys of testConfig.
type server struct {
	Host string
}

// testConfig is the configuration the tests load: keys "name", "port", "retries" and
// "server.host".
type testConfig struct {
	Name    string
	Port    int
	Retries int
	Server  server
}

// Validate requires a name and a port below 65536, reporting each problem as a FieldError.
func (c *testConfig) Validate() error {
	var errs []error
	if c.Name == "" {
		errs = append(errs, &cfg.FieldError{Key: "name", Err: errors.New("must not be empty")})
	}
	if c.Port > 65535 {
		errs = append(errs, &cfg.FieldError{Key: "port", Err: errors.New("must be below 65536")})
	}
	return errors.Join(errs...)
}

func defaults() testConfig {
	return testConfig{Name: "tool", Port: 8080, Retries: 3, Server: server{Host: "localhost"}}
}

// options returns the Options of a configuration file named config.yaml in a new directory,
// holding content unless content is "", in which case no file is written.
func options(t *testing.T, content string) config.Options[testConfig] {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return config.Options[testConfig]{Path: path, Defaults: defaults(), EnvPrefix: "TOOL"}
}

// subcommand returns the subcommand of group named name, failing t when it has none.
func subcommand(t *testing.T, group *spec.Command, name string) *spec.Command {
	t.Helper()
	for _, c := range group.Commands {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("config group has no %q subcommand", name)
	return nil
}

// result is what one run of a subcommand wrote and returned.
type result struct {
	stdout, stderr string
	err            error
}

// run runs the subcommand name of the group for opts with ctx, colour off, writing JSON when
// asJSON is true.
func run(t *testing.T, ctx context.Context, opts config.Options[testConfig], name string, asJSON bool) result {
	t.Helper()
	var out, errOut bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdout: &out, Stderr: &errOut}, nil, nil)
	format := spec.FormatText
	if asJSON {
		format = spec.FormatJSON
	}
	inv.Output = spec.NewOutput(spec.OutputOptions{Format: format, NoColorStdout: true, NoColorStderr: true})
	err := subcommand(t, config.New(opts), name).Run(ctx, inv)
	return result{stdout: out.String(), stderr: errOut.String(), err: err}
}

func TestGroupHoldsItsSubcommands(t *testing.T) {
	group := config.New(options(t, ""))
	if group.Name != "config" {
		t.Errorf("Name = %q, want config", group.Name)
	}
	if group.Run != nil {
		t.Error("the group has a run function, want none so that it prints its help")
	}
	var names []string
	for _, c := range group.Commands {
		names = append(names, c.Name)
	}
	if got, want := strings.Join(names, " "), "path validate show edit"; got != want {
		t.Errorf("subcommands = %q, want %q", got, want)
	}
	root := &spec.Command{Name: "tool", Commands: []*spec.Command{group}}
	if err := root.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestSkipConfigOnEveryCommandButShow(t *testing.T) {
	group := config.New(options(t, ""))
	tests := []struct {
		name string
		skip bool
	}{
		{"path", true},
		{"validate", true},
		{"show", false},
		{"edit", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, skip := subcommand(t, group, tt.name).Annotations[spec.SkipConfig]
			if skip != tt.skip {
				t.Errorf("annotated %s = %t, want %t", spec.SkipConfig, skip, tt.skip)
			}
		})
	}
	if _, skip := group.Annotations[spec.SkipConfig]; skip {
		t.Errorf("the group is annotated %s, want only its subcommands", spec.SkipConfig)
	}
}

func TestGroupAlonePrintsHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	root := &spec.Command{Name: "tool", Commands: []*spec.Command{config.New(options(t, ""))}}
	code := cli.ExecuteArgs(&spec.Env{Stdout: &out, Stderr: &errOut}, root, []string{"config"})
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr %q", code, cli.ExitOK, errOut.String())
	}
	help := out.String() + errOut.String()
	for _, name := range []string{"path", "validate", "show", "edit"} {
		if !strings.Contains(help, name) {
			t.Errorf("help %q does not list %q", help, name)
		}
	}
}

func TestLoadLayersDefaultsFileAndEnv(t *testing.T) {
	opts := options(t, "port: 9090\n")
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
	got := res.Value()
	if got.Name != "tool" || got.Port != 9090 || got.Retries != 5 {
		t.Errorf("Value = %+v, want name tool, port 9090, retries 5", got)
	}
}

func TestLoadReadsTheInvocationEnvWithoutLookupEnv(t *testing.T) {
	opts := options(t, "")
	env := &spec.Env{LookupEnv: func(key string) (string, bool) {
		if key == "TOOL_PORT" {
			return "7070", true
		}
		return "", false
	}}
	res, err := opts.Load(spec.NewInvocation(env, nil, nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := res.Value().Port; got != 7070 {
		t.Errorf("Port = %d, want 7070", got)
	}
}

// writeFile writes content to a file named name in a new directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// overlayAt returns an Overlay that resolves to path.
func overlayAt(path string) func(func(string) (string, bool)) (string, error) {
	return func(func(string) (string, bool)) (string, error) { return path, nil }
}

func TestLoadLayersTheOverlayBetweenTheFileAndEnv(t *testing.T) {
	opts := options(t, "name: file\nport: 9090\nretries: 4\n")
	overlay := writeFile(t, "overlay.yaml", "port: 7070\nretries: 6\n")
	var looked []string
	opts.LookupEnv = func(key string) (string, bool) {
		looked = append(looked, key)
		switch key {
		case "OVERLAY":
			return overlay, true
		case "TOOL_RETRIES":
			return "5", true
		}
		return "", false
	}
	opts.Overlay = func(lookup func(string) (string, bool)) (string, error) {
		path, _ := lookup("OVERLAY")
		return path, nil
	}
	res, err := opts.Load(spec.NewInvocation(nil, nil, nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := res.Value()
	if got.Name != "file" || got.Port != 7070 || got.Retries != 5 {
		t.Errorf("Value = %+v, want name file, port 7070, retries 5", got)
	}
	if len(looked) == 0 || looked[0] != "OVERLAY" {
		t.Errorf("looked up %q, want OVERLAY through LookupEnv first", looked)
	}
}

func TestOverlayReadsTheInvocationEnvWithoutLookupEnv(t *testing.T) {
	opts := options(t, "")
	overlay := writeFile(t, "overlay.yaml", "port: 7070\n")
	opts.Overlay = func(lookup func(string) (string, bool)) (string, error) {
		path, _ := lookup("OVERLAY")
		return path, nil
	}
	env := &spec.Env{LookupEnv: func(key string) (string, bool) {
		if key == "OVERLAY" {
			return overlay, true
		}
		return "", false
	}}
	inv := spec.NewInvocation(env, nil, nil)
	if path, err := opts.OverlayPath(inv); path != overlay || err != nil {
		t.Errorf("OverlayPath = %q, %v; want %q", path, err, overlay)
	}
	res, err := opts.Load(inv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := res.Value().Port; got != 7070 {
		t.Errorf("Port = %d, want 7070", got)
	}
}

func TestOverlayPathWithoutAnOverlayFile(t *testing.T) {
	tests := []struct {
		name    string
		overlay func(t *testing.T) func(func(string) (string, bool)) (string, error)
	}{
		{name: "no overlay", overlay: func(*testing.T) func(func(string) (string, bool)) (string, error) { return nil }},
		{name: "empty path", overlay: func(*testing.T) func(func(string) (string, bool)) (string, error) { return overlayAt("") }},
		{name: "missing file", overlay: func(t *testing.T) func(func(string) (string, bool)) (string, error) {
			return overlayAt(filepath.Join(t.TempDir(), "overlay.yaml"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := options(t, "port: 9090\n")
			opts.Overlay = tt.overlay(t)
			inv := spec.NewInvocation(nil, nil, nil)
			if path, err := opts.OverlayPath(inv); path != "" || err != nil {
				t.Errorf("OverlayPath = %q, %v; want no overlay", path, err)
			}
			res, err := opts.Load(inv)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := res.Value(); got.Port != 9090 || got.Retries != 3 {
				t.Errorf("Value = %+v, want port 9090 from the file and retries 3", got)
			}
		})
	}
}

func TestOverlayPathRunsTheResolverOnEveryCall(t *testing.T) {
	opts := options(t, "")
	paths := []string{
		writeFile(t, "first.yaml", "port: 1111\n"),
		writeFile(t, "second.yaml", "port: 2222\n"),
	}
	var calls int
	opts.Overlay = func(func(string) (string, bool)) (string, error) {
		calls++
		return paths[(calls-1)%len(paths)], nil
	}
	inv := spec.NewInvocation(nil, nil, nil)
	for i, want := range paths {
		if path, err := opts.OverlayPath(inv); path != want || err != nil {
			t.Errorf("call %d: OverlayPath = %q, %v; want %q", i+1, path, err, want)
		}
	}
	for i, want := range []int{1111, 2222} {
		res, err := opts.Load(inv)
		if err != nil {
			t.Fatalf("Load %d: %v", i+1, err)
		}
		if got := res.Value().Port; got != want {
			t.Errorf("Load %d: Port = %d, want %d", i+1, got, want)
		}
	}
	if calls != 4 {
		t.Errorf("resolver called %d times, want 4", calls)
	}
}

func TestOverlayResolverErrorFailsTheLoad(t *testing.T) {
	opts := options(t, "")
	errResolve := errors.New("no profile")
	opts.Overlay = func(func(string) (string, bool)) (string, error) { return "", errResolve }
	inv := spec.NewInvocation(nil, nil, nil)
	path, err := opts.OverlayPath(inv)
	if path != "" || !errors.Is(err, errResolve) || err.Error() != "config overlay: no profile" {
		t.Errorf("OverlayPath = %q, %v; want the resolver's error naming the overlay", path, err)
	}
	if _, err := opts.Load(inv); !errors.Is(err, errResolve) || err.Error() != "config overlay: no profile" {
		t.Errorf("Load error = %v, want the resolver's error naming the overlay", err)
	}
}

func TestOverlayUnreadableFailsTheLoad(t *testing.T) {
	t.Run("path through a regular file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows reports a path through a regular file as missing")
		}
		opts := options(t, "")
		bad := filepath.Join(writeFile(t, "regular", ""), "overlay.yaml")
		opts.Overlay = overlayAt(bad)
		inv := spec.NewInvocation(nil, nil, nil)
		path, err := opts.OverlayPath(inv)
		if path != "" || err == nil || !strings.HasPrefix(err.Error(), "config overlay "+bad+": ") {
			t.Errorf("OverlayPath = %q, %v; want the stat error naming the overlay %s", path, err, bad)
		}
		if _, err := opts.Load(inv); err == nil || !strings.HasPrefix(err.Error(), "config overlay "+bad+": ") {
			t.Errorf("Load error = %v, want the stat error naming the overlay %s", err, bad)
		}
	})
	t.Run("directory", func(t *testing.T) {
		opts := options(t, "")
		dir := t.TempDir()
		opts.Overlay = overlayAt(dir)
		inv := spec.NewInvocation(nil, nil, nil)
		if path, err := opts.OverlayPath(inv); path != dir || err != nil {
			t.Errorf("OverlayPath = %q, %v; want %q", path, err, dir)
		}
		if _, err := opts.Load(inv); err == nil || !strings.Contains(err.Error(), "config file "+dir+": ") {
			t.Errorf("Load error = %v, want the read error naming %s", err, dir)
		}
	})
}

func TestOverlayAllowRestrictsItsKeys(t *testing.T) {
	opts := options(t, "")
	opts.Overlay = overlayAt(writeFile(t, "overlay.yaml", "port: 7070\nretries: 6\nserver:\n  host: example\n"))
	opts.OverlayAllow = []string{"port", "server"}
	_, err := opts.Load(spec.NewInvocation(nil, nil, nil))
	if err == nil {
		t.Fatal("Load succeeded, want retries refused")
	}
	if msg := err.Error(); !strings.Contains(msg, "overlay.yaml:2") || !strings.Contains(msg, "not settable in overlay.yaml") ||
		strings.Contains(msg, "overlay.yaml:1") || strings.Contains(msg, "overlay.yaml:4") {
		t.Errorf("Load error = %q, want only line 2 not settable in overlay.yaml", msg)
	}

	opts.OverlayAllow = nil
	res, err := opts.Load(spec.NewInvocation(nil, nil, nil))
	if err != nil {
		t.Fatalf("Load with no allow-list: %v", err)
	}
	if got := res.Value(); got.Port != 7070 || got.Retries != 6 || got.Server.Host != "example" {
		t.Errorf("Value = %+v, want every key of the overlay", got)
	}
}

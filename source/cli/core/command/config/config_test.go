package config_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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

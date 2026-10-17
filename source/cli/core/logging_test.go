package cli_test

import (
	"context"
	"strings"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// loggingApp returns an App with opts and a "run" command that records that it ran in ran.
func loggingApp(ran *bool, opts ...cli.Option) *cli.App {
	app := cli.New(identity(), opts...)
	app.Add(&spec.Command{Name: "run", Run: func(context.Context, *spec.Invocation) error {
		*ran = true
		return nil
	}})
	return app
}

func TestWithLoggingDeclaresInheritedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "root help", args: []string{"--help"}},
		{name: "subcommand help", args: []string{"run", "--help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ran bool
			app := loggingApp(&ran, cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging())
			got := runApp(t, app, tt.args...)
			if got.code != cli.ExitOK || got.stderr != "" {
				t.Fatalf("code %d, stderr %q; want 0 and no stderr", got.code, got.stderr)
			}
			for _, s := range []string{"--log-level string", "--log-format string", `(default "text")`} {
				if !strings.Contains(got.stdout, s) {
					t.Errorf("help lacks %q:\n%s", s, got.stdout)
				}
			}
			for _, line := range strings.Split(got.stdout, "\n") {
				if strings.Contains(line, "--log-level") && strings.Contains(line, "(default") {
					t.Errorf("--log-level shows a default: %q", line)
				}
			}
		})
	}
}

func TestWithLoggingRequiresVerboseAndQuiet(t *testing.T) {
	tests := []struct {
		name string
		opts []cli.Option
	}{
		{name: "without WithVerbose", opts: []cli.Option{cli.WithQuiet(), cli.WithLogging()}},
		{name: "without WithQuiet", opts: []cli.Option{cli.WithLogging(), cli.WithVerbose()}},
		{name: "without either", opts: []cli.Option{cli.WithLogging()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ran bool
			got := runApp(t, loggingApp(&ran, tt.opts...), "run")
			if got.code != cli.ExitError || got.stdout != "" {
				t.Errorf("code %d, stdout %q; want 1 and no stdout", got.code, got.stdout)
			}
			want := "tool: invalid command tree\n" +
				"  → WithLogging requires WithVerbose and WithQuiet\n"
			if got.stderr != want {
				t.Errorf("stderr %q, want %q", got.stderr, want)
			}
			if ran {
				t.Error("run function called, want none")
			}
		})
	}
}

func TestWithLoggingWithVerboseAndQuietRuns(t *testing.T) {
	var ran bool
	app := loggingApp(&ran, cli.WithLogging(), cli.WithVerbose(), cli.WithQuiet())
	got := runApp(t, app, "run", "--log-level", "debug", "--log-format", "json")
	if got.code != cli.ExitOK || got.stdout != "" || got.stderr != "" {
		t.Errorf("result %+v, want 0 and no output", got)
	}
	if !ran {
		t.Error("run function not called")
	}
}

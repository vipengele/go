package cli_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// stdoutTerminal is an Env.Terminal under which only the standard output is a terminal.
func stdoutTerminal(s spec.Stream) bool { return s == spec.Stdout }

// stderrTerminal is an Env.Terminal under which only the standard error is a terminal.
func stderrTerminal(s spec.Stream) bool { return s == spec.Stderr }

// noTerminal is an Env.Terminal under which no standard stream is a terminal.
func noTerminal(spec.Stream) bool { return false }

// outputApp returns an App configured by opts whose command "show" stores the Output it
// receives in *got.
func outputApp(got *spec.Output, opts ...cli.Option) *cli.App {
	app := cli.New(identity(), opts...)
	app.Add(&spec.Command{
		Name:    "show",
		Summary: "Record the output settings",
		Run: func(_ context.Context, inv *spec.Invocation) error {
			*got = inv.Output
			return nil
		},
	})
	return app
}

func lookup(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestAppOutputColor(t *testing.T) {
	tests := []struct {
		name     string
		opts     []cli.Option
		args     []string
		vars     map[string]string
		stdout   io.Writer
		terminal func(spec.Stream) bool
		want     bool
	}{
		{
			name:     "terminal allows colour",
			stdout:   &bytes.Buffer{},
			terminal: stdoutTerminal,
			want:     true,
		},
		{
			name:     "stdout that is not a terminal forbids colour",
			stdout:   &bytes.Buffer{},
			terminal: noTerminal,
		},
		{
			name:     "a terminal on stderr alone forbids colour",
			stdout:   &bytes.Buffer{},
			terminal: stderrTerminal,
		},
		{
			name:   "writer that is not a file forbids colour",
			stdout: &bytes.Buffer{},
		},
		{
			name: "nil stdout forbids colour",
		},
		{
			name:     "--no-color forbids colour",
			opts:     []cli.Option{cli.WithNoColor()},
			args:     []string{"--no-color"},
			terminal: stdoutTerminal,
		},
		{
			name:     "--no-color absent allows colour",
			opts:     []cli.Option{cli.WithNoColor()},
			terminal: stdoutTerminal,
			want:     true,
		},
		{
			name:     "NO_COLOR forbids colour",
			vars:     map[string]string{"NO_COLOR": "1"},
			terminal: stdoutTerminal,
		},
		{
			name:     "empty NO_COLOR allows colour",
			vars:     map[string]string{"NO_COLOR": ""},
			terminal: stdoutTerminal,
			want:     true,
		},
		{
			name:     "prefixed NO_COLOR forbids colour",
			vars:     map[string]string{"TOOL_NO_COLOR": "yes"},
			terminal: stdoutTerminal,
		},
		{
			name:     "empty prefixed NO_COLOR allows colour",
			vars:     map[string]string{"TOOL_NO_COLOR": ""},
			terminal: stdoutTerminal,
			want:     true,
		},
		{
			name:     "another tool's NO_COLOR allows colour",
			vars:     map[string]string{"OTHER_NO_COLOR": "1"},
			terminal: stdoutTerminal,
			want:     true,
		},
		{
			name:     "--json keeps colour",
			opts:     []cli.Option{cli.WithJSON()},
			args:     []string{"--json"},
			terminal: stdoutTerminal,
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got spec.Output
			var stderr bytes.Buffer
			env := &spec.Env{
				Stdout:    tt.stdout,
				Stderr:    &stderr,
				LookupEnv: lookup(tt.vars),
				Terminal:  tt.terminal,
			}
			args := append([]string{"show"}, tt.args...)
			if code := outputApp(&got, tt.opts...).ExecuteArgs(env, args); code != cli.ExitOK {
				t.Fatalf("ExecuteArgs(%q) = %d, stderr %q; want 0", args, code, stderr.String())
			}
			if got.Color() != tt.want {
				t.Errorf("Color() = %t, want %t", got.Color(), tt.want)
			}
		})
	}
}

func TestExecuteArgsOutputColor(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want bool
	}{
		{name: "terminal allows colour", want: true},
		{name: "NO_COLOR forbids colour", vars: map[string]string{"NO_COLOR": "1"}},
		{name: "a prefixed NO_COLOR is not read", vars: map[string]string{"TOOL_NO_COLOR": "1"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got spec.Output
			root := &spec.Command{
				Name: "tool",
				Run: func(_ context.Context, inv *spec.Invocation) error {
					got = inv.Output
					return nil
				},
			}
			env := &spec.Env{LookupEnv: lookup(tt.vars), Terminal: stdoutTerminal}
			if code := cli.ExecuteArgs(env, root, nil); code != cli.ExitOK {
				t.Fatalf("ExecuteArgs = %d, want 0", code)
			}
			if got.Color() != tt.want {
				t.Errorf("Color() = %t, want %t", got.Color(), tt.want)
			}
		})
	}
}

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

// bothTerminal is an Env.Terminal under which the standard output and the standard error are
// terminals.
func bothTerminal(s spec.Stream) bool { return s == spec.Stdout || s == spec.Stderr }

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
		name       string
		opts       []cli.Option
		args       []string
		vars       map[string]string
		stdout     io.Writer
		terminal   func(spec.Stream) bool
		wantStdout bool
		wantStderr bool
	}{
		{
			name:       "terminals allow colour on both streams",
			stdout:     &bytes.Buffer{},
			terminal:   bothTerminal,
			wantStdout: true,
			wantStderr: true,
		},
		{
			name:     "no terminal forbids colour on both streams",
			stdout:   &bytes.Buffer{},
			terminal: noTerminal,
		},
		{
			name:       "a redirected stdout forbids colour on stdout alone",
			stdout:     &bytes.Buffer{},
			terminal:   stderrTerminal,
			wantStderr: true,
		},
		{
			name:       "a redirected stderr forbids colour on stderr alone",
			stdout:     &bytes.Buffer{},
			terminal:   stdoutTerminal,
			wantStdout: true,
		},
		{
			name:   "writers that are not files forbid colour",
			stdout: &bytes.Buffer{},
		},
		{
			name: "nil stdout forbids colour",
		},
		{
			name:     "--no-color forbids colour on both streams",
			opts:     []cli.Option{cli.WithNoColor()},
			args:     []string{"--no-color"},
			terminal: bothTerminal,
		},
		{
			name:       "--no-color absent allows colour",
			opts:       []cli.Option{cli.WithNoColor()},
			terminal:   bothTerminal,
			wantStdout: true,
			wantStderr: true,
		},
		{
			name:     "NO_COLOR forbids colour",
			vars:     map[string]string{"NO_COLOR": "1"},
			terminal: bothTerminal,
		},
		{
			name:       "empty NO_COLOR allows colour",
			vars:       map[string]string{"NO_COLOR": ""},
			terminal:   bothTerminal,
			wantStdout: true,
			wantStderr: true,
		},
		{
			name:     "prefixed NO_COLOR forbids colour",
			vars:     map[string]string{"TOOL_NO_COLOR": "yes"},
			terminal: bothTerminal,
		},
		{
			name:       "empty prefixed NO_COLOR allows colour",
			vars:       map[string]string{"TOOL_NO_COLOR": ""},
			terminal:   bothTerminal,
			wantStdout: true,
			wantStderr: true,
		},
		{
			name:       "another tool's NO_COLOR allows colour",
			vars:       map[string]string{"OTHER_NO_COLOR": "1"},
			terminal:   bothTerminal,
			wantStdout: true,
			wantStderr: true,
		},
		{
			name:       "FORCE_COLOR allows colour on streams that are not terminals",
			vars:       map[string]string{"FORCE_COLOR": "1"},
			terminal:   noTerminal,
			wantStdout: true,
			wantStderr: true,
		},
		{
			name:       "--json keeps colour",
			opts:       []cli.Option{cli.WithJSON()},
			args:       []string{"--json"},
			terminal:   bothTerminal,
			wantStdout: true,
			wantStderr: true,
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
			if got.Color(spec.Stdout) != tt.wantStdout {
				t.Errorf("Color(Stdout) = %t, want %t", got.Color(spec.Stdout), tt.wantStdout)
			}
			if got.Color(spec.Stderr) != tt.wantStderr {
				t.Errorf("Color(Stderr) = %t, want %t", got.Color(spec.Stderr), tt.wantStderr)
			}
		})
	}
}

func TestExecuteArgsOutputColor(t *testing.T) {
	tests := []struct {
		name                   string
		vars                   map[string]string
		terminal               func(spec.Stream) bool
		wantStdout, wantStderr bool
	}{
		{name: "terminals allow colour", terminal: bothTerminal, wantStdout: true, wantStderr: true},
		{name: "a redirected stdout forbids colour on stdout alone", terminal: stderrTerminal, wantStderr: true},
		{name: "a redirected stderr forbids colour on stderr alone", terminal: stdoutTerminal, wantStdout: true},
		{name: "NO_COLOR forbids colour", vars: map[string]string{"NO_COLOR": "1"}, terminal: bothTerminal},
		{
			name:       "a prefixed NO_COLOR is not read",
			vars:       map[string]string{"TOOL_NO_COLOR": "1"},
			terminal:   bothTerminal,
			wantStdout: true,
			wantStderr: true,
		},
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
			env := &spec.Env{LookupEnv: lookup(tt.vars), Terminal: tt.terminal}
			if code := cli.ExecuteArgs(env, root, nil); code != cli.ExitOK {
				t.Fatalf("ExecuteArgs = %d, want 0", code)
			}
			if got.Color(spec.Stdout) != tt.wantStdout {
				t.Errorf("Color(Stdout) = %t, want %t", got.Color(spec.Stdout), tt.wantStdout)
			}
			if got.Color(spec.Stderr) != tt.wantStderr {
				t.Errorf("Color(Stderr) = %t, want %t", got.Color(spec.Stderr), tt.wantStderr)
			}
		})
	}
}

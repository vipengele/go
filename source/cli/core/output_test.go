package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// terminal returns an *os.File open on a character device, which the Output treats as a
// terminal, and skips the test where the null device is not one.
func terminal(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device", os.DevNull)
	}
	return f
}

// regularFile returns an *os.File open on a regular file, which is not a terminal.
func regularFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

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
		name   string
		opts   []cli.Option
		args   []string
		vars   map[string]string
		stdout func(t *testing.T) io.Writer
		want   bool
	}{
		{
			name:   "terminal allows colour",
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
			want:   true,
		},
		{
			name:   "regular file forbids colour",
			stdout: func(t *testing.T) io.Writer { return regularFile(t) },
		},
		{
			name:   "writer that is not a file forbids colour",
			stdout: func(*testing.T) io.Writer { return &bytes.Buffer{} },
		},
		{
			name:   "nil stdout forbids colour",
			stdout: func(*testing.T) io.Writer { return nil },
		},
		{
			name:   "--no-color forbids colour",
			opts:   []cli.Option{cli.WithNoColor()},
			args:   []string{"--no-color"},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
		},
		{
			name:   "--no-color absent allows colour",
			opts:   []cli.Option{cli.WithNoColor()},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
			want:   true,
		},
		{
			name:   "NO_COLOR forbids colour",
			vars:   map[string]string{"NO_COLOR": "1"},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
		},
		{
			name:   "empty NO_COLOR allows colour",
			vars:   map[string]string{"NO_COLOR": ""},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
			want:   true,
		},
		{
			name:   "prefixed NO_COLOR forbids colour",
			vars:   map[string]string{"TOOL_NO_COLOR": "yes"},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
		},
		{
			name:   "empty prefixed NO_COLOR allows colour",
			vars:   map[string]string{"TOOL_NO_COLOR": ""},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
			want:   true,
		},
		{
			name:   "another tool's NO_COLOR allows colour",
			vars:   map[string]string{"OTHER_NO_COLOR": "1"},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
			want:   true,
		},
		{
			name:   "--json keeps colour",
			opts:   []cli.Option{cli.WithJSON()},
			args:   []string{"--json"},
			stdout: func(t *testing.T) io.Writer { return terminal(t) },
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got spec.Output
			var stderr bytes.Buffer
			env := &spec.Env{Stdout: tt.stdout(t), Stderr: &stderr, LookupEnv: lookup(tt.vars)}
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
			env := &spec.Env{Stdout: terminal(t), LookupEnv: lookup(tt.vars)}
			if code := cli.ExecuteArgs(env, root, nil); code != cli.ExitOK {
				t.Fatalf("ExecuteArgs = %d, want 0", code)
			}
			if got.Color() != tt.want {
				t.Errorf("Color() = %t, want %t", got.Color(), tt.want)
			}
		})
	}
}

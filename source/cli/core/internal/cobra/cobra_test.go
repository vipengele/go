package cobra_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/spec"
)

type ctxKey struct{}

func run(t *testing.T, root *spec.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := (&spec.Env{Stdout: &out, Stderr: &errOut}).Resolved()
	if verr := root.Validate(); verr != nil {
		t.Fatalf("invalid tree: %v", verr)
	}
	err = cobra.Run(context.Background(), env, root, args, cobra.Config{})
	return out.String(), errOut.String(), err
}

func TestUsageError(t *testing.T) {
	cause := errors.New("bad")
	tests := []struct {
		err  *cobra.UsageError
		want string
	}{
		{&cobra.UsageError{Err: cause}, "bad"},
		{&cobra.UsageError{Path: "remote add", Err: cause}, "remote add: bad"},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() = %q, want %q", got, tt.want)
		}
		if !errors.Is(tt.err, cause) {
			t.Errorf("errors.Is(%v, cause) = false", tt.err)
		}
	}
}

func TestRunArgCountErrorIsAUsageError(t *testing.T) {
	root := &spec.Command{
		Name: "tool",
		Args: spec.ExactArgs(1),
		Run:  func(context.Context, *spec.Invocation) error { return nil },
	}
	_, _, err := run(t, root)
	var usage *cobra.UsageError
	if !errors.As(err, &usage) || usage.Path != "" {
		t.Fatalf("Run = %v, want a *UsageError of the root", err)
	}
	var count *spec.ArgCountError
	if !errors.As(err, &count) || count.Got != 0 {
		t.Errorf("Run = %v, want a *spec.ArgCountError with Got 0", err)
	}
}

func TestRunNilArgsIsNoArguments(t *testing.T) {
	var got []string
	root := &spec.Command{
		Name: "tool",
		Args: spec.AnyArgs(),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			got = inv.Args
			return nil
		},
	}
	env := (&spec.Env{}).Resolved()
	if err := cobra.Run(context.Background(), env, root, nil, cobra.Config{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Run with nil args passed %q, want none", got)
	}
}

func TestRunPassesContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	var got any
	root := &spec.Command{
		Name: "tool",
		Run: func(ctx context.Context, _ *spec.Invocation) error {
			got = ctx.Value(ctxKey{})
			return nil
		},
	}
	env := (&spec.Env{}).Resolved()
	if err := cobra.Run(ctx, env, root, []string{}, cobra.Config{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "marker" {
		t.Errorf("run saw context value %v, want %q", got, "marker")
	}
}

func TestRunReturnsTheRunError(t *testing.T) {
	boom := errors.New("boom")
	root := &spec.Command{
		Name: "tool",
		Run:  func(context.Context, *spec.Invocation) error { return boom },
	}
	_, stderr, err := run(t, root)
	var usage *cobra.UsageError
	if !errors.Is(err, boom) || errors.As(err, &usage) {
		t.Errorf("Run = %v, want boom and no *UsageError", err)
	}
	if stderr != "" {
		t.Errorf("stderr %q, want nothing: the caller reports the error", stderr)
	}
}

func TestRunShorthandH(t *testing.T) {
	host := spec.StringFlag("host", "the host", spec.Shorthand("h"), spec.Inherited())
	leaf := &spec.Command{
		Name: "leaf",
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, _ = fmt.Fprintln(inv.Env.Stdout, host.Get(inv))
			return nil
		},
	}
	root := &spec.Command{
		Name:     "tool",
		Flags:    []spec.Flag{host},
		Commands: []*spec.Command{leaf},
	}

	stdout, _, err := run(t, root, "leaf", "-h", "example.com")
	if err != nil || stdout != "example.com\n" {
		t.Errorf("leaf -h example.com: stdout %q, err %v; want the host", stdout, err)
	}
	stdout, _, err = run(t, root, "leaf", "--help")
	if err != nil || !strings.Contains(stdout, "--help") {
		t.Errorf("leaf --help: stdout %q, err %v; want help", stdout, err)
	}
}

func TestRunFlagNamedHelp(t *testing.T) {
	ran := false
	root := &spec.Command{
		Name:  "tool",
		Flags: []spec.Flag{spec.BoolFlag("help", "a flag of the tool's own")},
		Run: func(context.Context, *spec.Invocation) error {
			ran = true
			return nil
		},
	}
	_, _, err := run(t, root)
	var usage *cobra.UsageError
	if err == nil || errors.As(err, &usage) {
		t.Errorf("Run = %v, want an error that is not a usage error", err)
	}
	if ran {
		t.Error("the run function ran")
	}
}

func TestRunFlagDefaults(t *testing.T) {
	b := spec.BoolFlag("b", "", spec.Default(true))
	s := spec.StringFlag("s", "", spec.Default("def"))
	i := spec.IntFlag("i", "", spec.Default(7))
	root := &spec.Command{
		Name:  "tool",
		Flags: []spec.Flag{b, s, i},
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, _ = fmt.Fprintln(inv.Env.Stdout, b.Get(inv), s.Get(inv), i.Get(inv))
			return nil
		},
	}
	tests := []struct {
		args []string
		want string
	}{
		{nil, "true def 7\n"},
		{[]string{"--b=false", "--s", "x", "--i=3"}, "false x 3\n"},
	}
	for _, tt := range tests {
		stdout, _, err := run(t, root, tt.args...)
		if err != nil || stdout != tt.want {
			t.Errorf("Run(%q): stdout %q, err %v; want %q", tt.args, stdout, err, tt.want)
		}
	}
}

func TestRunCommandWithRunAndSubcommands(t *testing.T) {
	root := &spec.Command{
		Name: "tool",
		Args: spec.RangeArgs(1, 1),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, _ = fmt.Fprintln(inv.Env.Stdout, "root", inv.Args)
			return nil
		},
		Commands: []*spec.Command{{
			Name: "sub",
			Run: func(_ context.Context, inv *spec.Invocation) error {
				_, _ = fmt.Fprintln(inv.Env.Stdout, "sub")
				return nil
			},
		}},
	}
	tests := []struct {
		args    []string
		stdout  string
		wantErr string
	}{
		{[]string{"sub"}, "sub\n", ""},
		{[]string{"x"}, "root [x]\n", ""},
		{nil, "", "accepts exactly 1 argument, received 0 arguments"},
		{[]string{"x", "y"}, "", `unknown command "x" for "tool"`},
	}
	for _, tt := range tests {
		stdout, _, err := run(t, root, tt.args...)
		if stdout != tt.stdout {
			t.Errorf("Run(%q): stdout %q, want %q", tt.args, stdout, tt.stdout)
		}
		var usage *cobra.UsageError
		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("Run(%q) = %v, want nil", tt.args, err)
		case tt.wantErr != "" && (!errors.As(err, &usage) || err.Error() != tt.wantErr):
			t.Errorf("Run(%q) = %v, want the usage error %q", tt.args, err, tt.wantErr)
		}
	}
}

func TestRunConfigOutput(t *testing.T) {
	quiet := spec.BoolFlag("quiet", "say less", spec.Inherited())
	tests := []struct {
		name      string
		args      []string
		output    func(inv *spec.Invocation) (spec.Output, error)
		wantQuiet bool
		wantColor bool
		wantErr   string
		wantRun   bool
	}{
		{
			name:      "nil resolver leaves the zero output",
			args:      []string{"leaf", "--quiet"},
			wantColor: true,
			wantRun:   true,
		},
		{
			name: "resolver sees the parsed flags",
			args: []string{"leaf", "--quiet"},
			output: func(inv *spec.Invocation) (spec.Output, error) {
				return spec.NewOutput(spec.OutputOptions{Quiet: quiet.Get(inv), NoColor: true}), nil
			},
			wantQuiet: true,
			wantRun:   true,
		},
		{
			name: "resolver error is a usage error of the command",
			args: []string{"leaf"},
			output: func(*spec.Invocation) (spec.Output, error) {
				return spec.Output{}, errors.New("conflict")
			},
			wantColor: true,
			wantErr:   "leaf: conflict",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				ran bool
				got spec.Output
			)
			root := &spec.Command{
				Name:  "tool",
				Flags: []spec.Flag{quiet},
				Commands: []*spec.Command{{
					Name: "leaf",
					Run: func(_ context.Context, inv *spec.Invocation) error {
						ran, got = true, inv.Output
						return nil
					},
				}},
			}
			env := (&spec.Env{}).Resolved()
			err := cobra.Run(context.Background(), env, root, tt.args, cobra.Config{Output: tt.output})
			if tt.wantErr != "" {
				var usage *cobra.UsageError
				if !errors.As(err, &usage) || err.Error() != tt.wantErr {
					t.Fatalf("Run(%q) = %v, want the usage error %q", tt.args, err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Run(%q) = %v, want nil", tt.args, err)
			}
			if ran != tt.wantRun {
				t.Fatalf("run function ran: %t, want %t", ran, tt.wantRun)
			}
			if got.Quiet() != tt.wantQuiet || got.Color() != tt.wantColor {
				t.Errorf("Output quiet=%t color=%t, want quiet=%t color=%t",
					got.Quiet(), got.Color(), tt.wantQuiet, tt.wantColor)
			}
		})
	}
}

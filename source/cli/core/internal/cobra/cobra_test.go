package cobra_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
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

func TestRunFlagNamedHelpOnASubcommand(t *testing.T) {
	ran := false
	root := &spec.Command{
		Name: "tool",
		Commands: []*spec.Command{{
			Name:  "leaf",
			Flags: []spec.Flag{spec.BoolFlag("help", "a flag of the tool's own")},
			Run: func(context.Context, *spec.Invocation) error {
				ran = true
				return nil
			},
		}},
	}
	_, _, err := run(t, root, "leaf")
	if err == nil {
		t.Fatal("Run succeeded, want the reserved --help refused")
	}
	if want := `command "leaf": flag --help is reserved for help`; err.Error() != want {
		t.Errorf("Run = %q, want %q", err, want)
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

func TestRunReportsTheExplicitFlags(t *testing.T) {
	b := spec.BoolFlag("b", "", spec.Inherited())
	s := spec.StringFlag("s", "", spec.Default("def"))
	i := spec.IntFlag("i", "")
	sub := &spec.Command{
		Name:  "sub",
		Flags: []spec.Flag{s, i},
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, _ = fmt.Fprintln(inv.Env.Stdout, b.IsSet(inv), s.IsSet(inv), i.IsSet(inv))
			return nil
		},
	}
	root := &spec.Command{Name: "tool", Flags: []spec.Flag{b}, Commands: []*spec.Command{sub}}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"sub"}, "false false false\n"},
		{[]string{"sub", "--b=false", "--s", "def"}, "true true false\n"},
		{[]string{"--b", "sub", "--i=0"}, "true false true\n"},
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
				return spec.NewOutput(spec.OutputOptions{Quiet: quiet.Get(inv), NoColorStdout: true, NoColorStderr: true}), nil
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
			if got.Quiet() != tt.wantQuiet || got.Color(spec.Stdout) != tt.wantColor ||
				got.Color(spec.Stderr) != tt.wantColor {
				t.Errorf("Output quiet=%t color stdout=%t stderr=%t, want quiet=%t color=%t",
					got.Quiet(), got.Color(spec.Stdout), got.Color(spec.Stderr), tt.wantQuiet, tt.wantColor)
			}
		})
	}
}

// hookRoot returns a tree whose command "remote add" records its run in *log, as "run", and
// returns runErr. Its parent "remote" has no Run.
func hookRoot(log *[]string, runErr error) *spec.Command {
	return &spec.Command{
		Name: "tool",
		Commands: []*spec.Command{{
			Name:        "remote",
			Annotations: map[string]string{"parent": "yes"},
			Commands: []*spec.Command{{
				Name:        "add",
				Args:        spec.ExactArgs(1),
				Annotations: map[string]string{"k": "v"},
				Run: func(context.Context, *spec.Invocation) error {
					*log = append(*log, "run")
					return runErr
				},
			}},
		}},
	}
}

// hooks returns a Config whose PreRun and PostRun record their calls in *log, as "pre<i>" and
// "post<i>", and return the errors of preErrs and postErrs at the same index.
func hooks(log *[]string, preErrs, postErrs []error) cobra.Config {
	var cfg cobra.Config
	for i, e := range preErrs {
		cfg.PreRun = append(cfg.PreRun, func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
			*log = append(*log, fmt.Sprintf("pre%d", i))
			return ctx, e
		})
	}
	for i, e := range postErrs {
		cfg.PostRun = append(cfg.PostRun, func(_ context.Context, _ *spec.Invocation, _ spec.CommandInfo, runErr error) error {
			*log = append(*log, fmt.Sprintf("post%d(%v)", i, runErr))
			return e
		})
	}
	return cfg
}

func TestRunConfigHooks(t *testing.T) {
	errBoom, errPre, errPost0, errPost1 := errors.New("boom"), errors.New("pre"), errors.New("post0"), errors.New("post1")
	tests := []struct {
		name     string
		args     []string
		runErr   error
		preErrs  []error
		postErrs []error
		wantLog  string
		wantErr  error
		wantHook bool
	}{
		{
			name:     "pre in order, post in reverse",
			args:     []string{"remote", "add", "x"},
			preErrs:  []error{nil, nil},
			postErrs: []error{nil, nil},
			wantLog:  "pre0 pre1 run post1(<nil>) post0(<nil>)",
		},
		{
			name:     "pre error stops the run and every post",
			args:     []string{"remote", "add", "x"},
			preErrs:  []error{errPre, nil},
			postErrs: []error{nil},
			wantLog:  "pre0",
			wantErr:  errPre,
		},
		{
			name:     "run error reaches every post and is returned",
			args:     []string{"remote", "add", "x"},
			runErr:   errBoom,
			postErrs: []error{errPost0, nil},
			wantLog:  "run post1(boom) post0(boom)",
			wantErr:  errBoom,
		},
		{
			name:     "first post error after a successful run, the rest still run",
			args:     []string{"remote", "add", "x"},
			postErrs: []error{errPost0, errPost1},
			wantLog:  "run post1(<nil>) post0(<nil>)",
			wantErr:  errPost1,
			wantHook: true,
		},
		{
			name:     "no hooks on --help",
			args:     []string{"remote", "add", "--help"},
			preErrs:  []error{nil},
			postErrs: []error{nil},
		},
		{
			name:     "no hooks on a command without Run",
			args:     []string{"remote"},
			preErrs:  []error{nil},
			postErrs: []error{nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var log []string
			env := (&spec.Env{}).Resolved()
			err := cobra.Run(context.Background(), env, hookRoot(&log, tt.runErr), tt.args,
				hooks(&log, tt.preErrs, tt.postErrs))
			if got := strings.Join(log, " "); got != tt.wantLog {
				t.Errorf("calls %q, want %q", got, tt.wantLog)
			}
			var hookErr *cobra.HookError
			switch {
			case errors.As(err, &hookErr) != tt.wantHook:
				t.Errorf("Run(%q) = %#v, want a HookError: %t", tt.args, err, tt.wantHook)
			case tt.wantHook && hookErr.Err != tt.wantErr:
				t.Errorf("Run(%q) = a HookError holding %v, want %v", tt.args, hookErr.Err, tt.wantErr)
			case !tt.wantHook && !errors.Is(err, tt.wantErr):
				t.Errorf("Run(%q) = %v, want %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestRunConfigHooksSkipUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		output func(*spec.Invocation) (spec.Output, error)
	}{
		{name: "argument count", args: []string{"remote", "add"}},
		{name: "unknown flag", args: []string{"remote", "add", "x", "--bogus"}},
		{name: "unknown command", args: []string{"remote", "bogus"}},
		{
			name: "output error",
			args: []string{"remote", "add", "x"},
			output: func(*spec.Invocation) (spec.Output, error) {
				return spec.Output{}, errors.New("conflict")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			cfg := hooks(&log, []error{nil}, []error{nil})
			cfg.Output = tc.output
			env := (&spec.Env{}).Resolved()
			err := cobra.Run(context.Background(), env, hookRoot(&log, nil), tc.args, cfg)
			var usage *cobra.UsageError
			if !errors.As(err, &usage) {
				t.Errorf("Run(%q) = %v, want a usage error", tc.args, err)
			}
			if len(log) != 0 {
				t.Errorf("calls %q, want none", log)
			}
		})
	}
}

func TestRunConfigPostRunUsageErrorIsAHookError(t *testing.T) {
	var log []string
	usage := &cobra.UsageError{Err: errors.New("bad")}
	env := (&spec.Env{}).Resolved()
	err := cobra.Run(context.Background(), env, hookRoot(&log, nil), []string{"remote", "add", "x"},
		hooks(&log, nil, []error{usage}))
	var gotUsage *cobra.UsageError
	if errors.As(err, &gotUsage) {
		t.Errorf("Run = %#v, found a usage error through it", err)
	}
	var hookErr *cobra.HookError
	if !errors.As(err, &hookErr) || hookErr.Err != usage || err.Error() != "bad" {
		t.Errorf("Run = %#v, want a HookError holding %v", err, usage)
	}
}

func TestRunConfigHooksReceiveTheCommand(t *testing.T) {
	var log []string
	root := hookRoot(&log, nil)
	var pre, post spec.CommandInfo
	var preOutput spec.Output
	cfg := cobra.Config{
		Output: func(*spec.Invocation) (spec.Output, error) {
			return spec.NewOutput(spec.OutputOptions{Quiet: true}), nil
		},
		PreRun: []func(context.Context, *spec.Invocation, spec.CommandInfo) (context.Context, error){
			func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
				pre, preOutput = cmd, inv.Output
				return ctx, nil
			},
		},
		PostRun: []func(context.Context, *spec.Invocation, spec.CommandInfo, error) error{
			func(_ context.Context, _ *spec.Invocation, cmd spec.CommandInfo, _ error) error {
				post = cmd
				return nil
			},
		},
	}
	env := (&spec.Env{}).Resolved()
	if err := cobra.Run(context.Background(), env, root, []string{"remote", "add", "x"}, cfg); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	for _, info := range []spec.CommandInfo{pre, post} {
		if got := info.String(); got != "tool remote add" {
			t.Errorf("path %q, want %q", got, "tool remote add")
		}
		if got := info.Annotations(); len(got) != 1 || got["k"] != "v" {
			t.Errorf("annotations %v, want only k=v", got)
		}
	}
	if !preOutput.Quiet() {
		t.Error("pre-run hook saw an invocation without its Output")
	}
	pre.Annotations()["k"] = "changed"
	if v := root.Commands[0].Commands[0].Annotations["k"]; v != "v" {
		t.Errorf("command annotation %q after a hook changed its copy, want %q", v, "v")
	}
}

// trail returns the string ctx carries under ctxKey, empty when it carries none.
func trail(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

func TestRunConfigPreRunContextReachesWhatFollows(t *testing.T) {
	errPre := errors.New("pre")
	// pre returns a PreRun that records the trail it receives as "pre<name>[<trail>]" and returns
	// err with a context whose trail gains name, or with a nil context when name is empty.
	pre := func(log *[]string, i int, name string, err error) func(context.Context, *spec.Invocation, spec.CommandInfo) (context.Context, error) {
		return func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
			*log = append(*log, fmt.Sprintf("pre%d[%s]", i, trail(ctx)))
			if name == "" {
				return nil, err
			}
			return context.WithValue(ctx, ctxKey{}, trail(ctx)+name), err
		}
	}
	tests := []struct {
		name    string
		names   []string
		errs    []error
		wantLog string
		wantErr error
	}{
		{
			name:    "each context reaches the next pre-run, the run and every post-run",
			names:   []string{"a", "b"},
			errs:    []error{nil, nil},
			wantLog: "pre0[] pre1[a] run[ab] post1[ab] post0[ab]",
		},
		{
			name:    "a nil context keeps the one the hook received",
			names:   []string{"a", "", "c"},
			errs:    []error{nil, nil, nil},
			wantLog: "pre0[] pre1[a] pre2[a] run[ac] post1[ac] post0[ac]",
		},
		{
			name:    "an error stops the run and its context reaches nothing",
			names:   []string{"a", "b", "c"},
			errs:    []error{nil, errPre, nil},
			wantLog: "pre0[] pre1[a]",
			wantErr: errPre,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var log []string
			root := &spec.Command{
				Name: "tool",
				Commands: []*spec.Command{{
					Name: "go",
					Run: func(ctx context.Context, _ *spec.Invocation) error {
						log = append(log, fmt.Sprintf("run[%s]", trail(ctx)))
						return nil
					},
				}},
			}
			var cfg cobra.Config
			for i, name := range tt.names {
				cfg.PreRun = append(cfg.PreRun, pre(&log, i, name, tt.errs[i]))
			}
			for i := range 2 {
				cfg.PostRun = append(cfg.PostRun, func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo, _ error) error {
					log = append(log, fmt.Sprintf("post%d[%s]", i, trail(ctx)))
					return nil
				})
			}
			env := (&spec.Env{}).Resolved()
			err := cobra.Run(context.Background(), env, root, []string{"go"}, cfg)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Run = %v, want %v", err, tt.wantErr)
			}
			if got := strings.Join(log, " "); got != tt.wantLog {
				t.Errorf("calls %q, want %q", got, tt.wantLog)
			}
		})
	}
}

// complete runs cobra's __complete request for words against root and returns the candidates
// and the directive it writes, which the completion protocol puts on the last line.
func complete(t *testing.T, root *spec.Command, cfg cobra.Config, words ...string) (candidates []string, directive string) {
	t.Helper()
	if err := root.Validate(); err != nil {
		t.Fatalf("invalid tree: %v", err)
	}
	var out bytes.Buffer
	env := (&spec.Env{Stdout: &out, Stderr: &bytes.Buffer{}}).Resolved()
	args := append([]string{"__complete"}, words...)
	if err := cobra.Run(context.Background(), env, root, args, cfg); err != nil {
		t.Fatalf("Run(%q) = %v", args, err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	return lines[:len(lines)-1], lines[len(lines)-1]
}

// Directives of the completion protocol, as cobra writes them.
const (
	directiveDefault    = ":0"
	directiveNoFileComp = ":4"
	directiveFileExt    = ":8"
	directiveDirs       = ":16"
)

// completionCases are the completions of every kind, each with the word completed, the
// candidates and the directive it writes, for a flag value or a positional argument alike.
func completionCases(seen *spec.CompletionInput) []struct {
	name          string
	completion    spec.Completion
	toComplete    string
	wantValues    []string
	wantDirective string
} {
	fn := func(_ context.Context, in spec.CompletionInput) ([]string, error) {
		*seen = in
		return []string{"alpha", "beta"}, nil
	}
	failing := func(context.Context, spec.CompletionInput) ([]string, error) {
		return []string{"never"}, errors.New("lookup failed")
	}
	return []struct {
		name          string
		completion    spec.Completion
		toComplete    string
		wantValues    []string
		wantDirective string
	}{
		{"choices", spec.CompleteChoices("json", "text", "jsonl"), "", []string{"json", "text", "jsonl"}, directiveNoFileComp},
		{"choices by prefix", spec.CompleteChoices("json", "text", "jsonl"), "js", []string{"json", "jsonl"}, directiveNoFileComp},
		{"choices none matching", spec.CompleteChoices("json"), "x", nil, directiveNoFileComp},
		{"files", spec.CompleteFiles(), "", nil, directiveDefault},
		{"files by extension", spec.CompleteFiles(".yaml", "yml"), "", []string{"yaml", "yml"}, directiveFileExt},
		{"dirs", spec.CompleteDirs(), "", nil, directiveDirs},
		{"func", spec.CompleteFunc(fn), "a", []string{"alpha", "beta"}, directiveNoFileComp},
		{"func error", spec.CompleteFunc(failing), "", nil, directiveNoFileComp},
	}
}

func TestRunCompletesFlagValues(t *testing.T) {
	var seen spec.CompletionInput
	for _, tt := range completionCases(&seen) {
		t.Run(tt.name, func(t *testing.T) {
			value := spec.StringFlag("value", "the value", spec.Complete(tt.completion))
			global := spec.StringFlag("global", "a global", spec.Complete(tt.completion), spec.Inherited())
			root := &spec.Command{
				Name:  "tool",
				Flags: []spec.Flag{global},
				Commands: []*spec.Command{{
					Name:  "set",
					Args:  spec.AnyArgs(),
					Flags: []spec.Flag{value},
					Run:   func(context.Context, *spec.Invocation) error { return nil },
				}},
			}
			for _, words := range [][]string{
				{"set", "first", "--value", tt.toComplete},
				{"set", "first", "--value=" + tt.toComplete},
				{"set", "first", "--global", tt.toComplete},
			} {
				seen = spec.CompletionInput{}
				got, directive := complete(t, root, cobra.Config{}, words...)
				want := tt.wantValues
				if !slices.Equal(got, want) || directive != tt.wantDirective {
					t.Errorf("complete %q = %q %s, want %q %s", words, got, directive, want, tt.wantDirective)
				}
				if tt.name == "func" {
					wantSeen := spec.CompletionInput{ToComplete: tt.toComplete, Args: []string{"first"}}
					if seen.ToComplete != wantSeen.ToComplete || !slices.Equal(seen.Args, wantSeen.Args) {
						t.Errorf("complete %q gave the func %+v, want %+v", words, seen, wantSeen)
					}
				}
			}
		})
	}
}

func TestRunCompletesFlagValuesWithoutDescriptions(t *testing.T) {
	root := &spec.Command{
		Name:  "tool",
		Flags: []spec.Flag{spec.StringFlag("mode", "the mode", spec.Complete(spec.CompleteChoices("fast", "slow")))},
		Run:   func(context.Context, *spec.Invocation) error { return nil },
	}
	var out bytes.Buffer
	env := (&spec.Env{Stdout: &out, Stderr: &bytes.Buffer{}}).Resolved()
	if err := cobra.Run(context.Background(), env, root, []string{"__completeNoDesc", "--mode", ""}, cobra.Config{}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "fast\nslow\n"+directiveNoFileComp+"\n"; got != want {
		t.Errorf("__completeNoDesc --mode = %q, want %q", got, want)
	}
}

func TestRunCompletesPositionalArguments(t *testing.T) {
	var seen spec.CompletionInput
	for _, tt := range completionCases(&seen) {
		t.Run(tt.name, func(t *testing.T) {
			root := &spec.Command{
				Name: "tool",
				Commands: []*spec.Command{{
					Name:          "open",
					Args:          spec.RangeArgs(0, 2),
					ArgCompletion: tt.completion,
					Flags:         []spec.Flag{spec.BoolFlag("force", "")},
					Run:           func(context.Context, *spec.Invocation) error { return nil },
				}},
			}
			seen = spec.CompletionInput{}
			got, directive := complete(t, root, cobra.Config{}, "open", "--force", "first", tt.toComplete)
			if !slices.Equal(got, tt.wantValues) || directive != tt.wantDirective {
				t.Errorf("complete = %q %s, want %q %s", got, directive, tt.wantValues, tt.wantDirective)
			}
			if tt.name == "func" {
				if seen.ToComplete != tt.toComplete || !slices.Equal(seen.Args, []string{"first"}) {
					t.Errorf("the func got %+v, want ToComplete %q and Args [first]", seen, tt.toComplete)
				}
			}

			got, directive = complete(t, root, cobra.Config{}, "open", "first", "second", tt.toComplete)
			if len(got) != 0 || directive != directiveNoFileComp {
				t.Errorf("complete past the last argument = %q %s, want nothing %s", got, directive, directiveNoFileComp)
			}
		})
	}
}

func TestRunCompletesTheRootsArguments(t *testing.T) {
	root := &spec.Command{
		Name:          "tool",
		Args:          spec.AnyArgs(),
		ArgCompletion: spec.CompleteChoices("one", "two"),
		Run:           func(context.Context, *spec.Invocation) error { return nil },
	}
	got, directive := complete(t, root, cobra.Config{}, "t")
	if !slices.Equal(got, []string{"two"}) || directive != directiveNoFileComp {
		t.Errorf("complete = %q %s, want [two] %s", got, directive, directiveNoFileComp)
	}
}

func TestRunCompletionFuncReceivesTheRunContext(t *testing.T) {
	var got any
	root := &spec.Command{
		Name: "tool",
		Args: spec.AnyArgs(),
		ArgCompletion: spec.CompleteFunc(func(ctx context.Context, _ spec.CompletionInput) ([]string, error) {
			got = ctx.Value(ctxKey{})
			return nil, nil
		}),
		Run: func(context.Context, *spec.Invocation) error { return nil },
	}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	env := (&spec.Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Resolved()
	if err := cobra.Run(ctx, env, root, []string{"__complete", ""}, cobra.Config{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "marker" {
		t.Errorf("completion func saw context value %v, want %q", got, "marker")
	}
}

func TestRunCompletionRunsNoHook(t *testing.T) {
	var log []string
	cfg := hooks(&log, []error{nil}, []error{nil})
	cfg.Output = func(*spec.Invocation) (spec.Output, error) {
		log = append(log, "output")
		return spec.Output{}, nil
	}
	cfg.Completion = true
	root := hookRoot(&log, nil)
	root.Commands[0].Commands[0].ArgCompletion = spec.CompleteChoices("one")
	got, directive := complete(t, root, cfg, "remote", "add", "")
	if !slices.Equal(got, []string{"one"}) || directive != directiveNoFileComp {
		t.Errorf("complete = %q %s, want [one] %s", got, directive, directiveNoFileComp)
	}
	if len(log) != 0 {
		t.Errorf("completion called %q, want nothing", log)
	}
}

func completionRoot() *spec.Command {
	return &spec.Command{
		Name: "tool",
		Commands: []*spec.Command{{
			Name: "go",
			Run:  func(context.Context, *spec.Invocation) error { return nil },
		}},
	}
}

func TestRunWithoutCompletionHasNoCompletionCommand(t *testing.T) {
	for _, args := range [][]string{{"completion"}, {"completion", "bash"}} {
		_, _, err := run(t, completionRoot(), args...)
		var usage *cobra.UsageError
		if !errors.As(err, &usage) || !strings.Contains(err.Error(), `unknown command "completion"`) {
			t.Errorf("Run(%q) = %v, want a *UsageError for the unknown command completion", args, err)
		}
	}
}

func TestRunWithCompletionWritesTheScript(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var out bytes.Buffer
			env := (&spec.Env{Stdout: &out, Stderr: &bytes.Buffer{}}).Resolved()
			err := cobra.Run(context.Background(), env, completionRoot(), []string{"completion", shell},
				cobra.Config{Completion: true})
			if err != nil {
				t.Fatalf("Run(completion %s) = %v", shell, err)
			}
			if !strings.Contains(out.String(), "__complete") {
				t.Errorf("completion %s wrote %q, want a script that calls __complete", shell, out.String())
			}
		})
	}
}

func TestRunCompletionListsTheCompletionCommandOnlyWhenSet(t *testing.T) {
	for _, set := range []bool{false, true} {
		got, _ := complete(t, completionRoot(), cobra.Config{Completion: set}, "c")
		if want := set; slices.Contains(got, "completion\tGenerate the autocompletion script for the specified shell") != want {
			t.Errorf("Completion %v: complete c = %q, want completion listed %v", set, got, want)
		}
	}
}

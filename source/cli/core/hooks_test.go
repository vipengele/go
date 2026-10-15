package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// calls records, in order, the calls of the hooks and run functions of one execution.
type calls struct {
	mu  sync.Mutex
	log []string
}

func (c *calls) add(format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.log = append(c.log, fmt.Sprintf(format, a...))
}

func (c *calls) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.log, " ")
}

// hookApp returns an App configured by opts whose command "remote add" records its run in c,
// as "run", and returns runErr. Its parent "remote" has no Run.
func hookApp(c *calls, runErr error, opts ...cli.Option) *cli.App {
	app := cli.New(identity(), opts...)
	app.Add(&spec.Command{
		Name:    "remote",
		Summary: "Manage remotes",
		Commands: []*spec.Command{{
			Name:    "add",
			Summary: "Add a remote",
			Args:    spec.ExactArgs(1),
			Run: func(context.Context, *spec.Invocation) error {
				c.add("run")
				return runErr
			},
		}},
	})
	return app
}

// preHook returns a pre-run hook that records its call in c, as "pre<name>", and returns err.
func preHook(c *calls, name string, err error) cli.Option {
	return cli.WithPreRun(func(context.Context, *spec.Invocation, spec.CommandInfo) error {
		c.add("pre%s", name)
		return err
	})
}

// postHook returns a post-run hook that records its call in c, as "post<name>(<runErr>)", and
// returns err.
func postHook(c *calls, name string, err error) cli.Option {
	return cli.WithPostRun(func(_ context.Context, _ *spec.Invocation, _ spec.CommandInfo, runErr error) error {
		c.add("post%s(%v)", name, runErr)
		return err
	})
}

func TestAppHooks(t *testing.T) {
	usage := spec.ExactArgs(1).Check(nil)
	tests := []struct {
		name    string
		runErr  error
		hooks   func(c *calls) []cli.Option
		want    result
		wantLog string
	}{
		{
			name: "pre-run in order, post-run in reverse",
			hooks: func(c *calls) []cli.Option {
				return []cli.Option{preHook(c, "A", nil), postHook(c, "A", nil),
					preHook(c, "B", nil), postHook(c, "B", nil)}
			},
			want:    result{code: cli.ExitOK},
			wantLog: "preA preB run postB(<nil>) postA(<nil>)",
		},
		{
			name: "pre-run error stops the run and every post-run hook",
			hooks: func(c *calls) []cli.Option {
				return []cli.Option{preHook(c, "A", errors.New("no")), preHook(c, "B", nil), postHook(c, "A", nil)}
			},
			want:    result{code: cli.ExitError, stderr: "tool: no\n"},
			wantLog: "preA",
		},
		{
			name:   "run error reaches every post-run hook and is reported over theirs",
			runErr: errors.New("boom"),
			hooks: func(c *calls) []cli.Option {
				return []cli.Option{postHook(c, "A", errors.New("a")), postHook(c, "B", errors.New("b"))}
			},
			want:    result{code: cli.ExitError, stderr: "tool: boom\n"},
			wantLog: "run postB(boom) postA(boom)",
		},
		{
			name: "post-run error after a successful run is reported, the rest still run",
			hooks: func(c *calls) []cli.Option {
				return []cli.Option{postHook(c, "A", errors.New("a")), postHook(c, "B", errors.New("b"))}
			},
			want:    result{code: cli.ExitError, stderr: "tool: b\n"},
			wantLog: "run postB(<nil>) postA(<nil>)",
		},
		{
			name: "post-run usage error is a run error",
			hooks: func(c *calls) []cli.Option {
				return []cli.Option{postHook(c, "A", usage)}
			},
			want:    result{code: cli.ExitError, stderr: "tool: " + usage.Error() + "\n"},
			wantLog: "run postA(<nil>)",
		},
		{
			name: "pre-run usage error is a run error",
			hooks: func(c *calls) []cli.Option {
				return []cli.Option{preHook(c, "A", usage)}
			},
			want:    result{code: cli.ExitError, stderr: "tool: " + usage.Error() + "\n"},
			wantLog: "preA",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c calls
			app := hookApp(&c, tt.runErr, tt.hooks(&c)...)
			if got := runApp(t, app, "remote", "add", "origin"); got != tt.want {
				t.Errorf("ExecuteArgs\n got: %+v\nwant: %+v", got, tt.want)
			}
			if got := c.String(); got != tt.wantLog {
				t.Errorf("calls %q, want %q", got, tt.wantLog)
			}
		})
	}
}

func TestAppHooksDoNotRunWithoutARun(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
	}{
		{name: "--help", args: []string{"remote", "add", "--help"}, code: cli.ExitOK},
		{name: "command without Run", args: []string{"remote"}, code: cli.ExitOK},
		{name: "no arguments", args: nil, code: cli.ExitOK},
		{name: "argument count", args: []string{"remote", "add"}, code: cli.ExitUsage},
		{name: "unknown flag", args: []string{"remote", "add", "x", "--bogus"}, code: cli.ExitUsage},
		{name: "unknown command", args: []string{"remote", "bogus"}, code: cli.ExitUsage},
		{name: "verbose with quiet", args: []string{"remote", "add", "x", "-v", "-q"}, code: cli.ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c calls
			app := hookApp(&c, nil, cli.WithVerbose(), cli.WithQuiet(),
				preHook(&c, "A", nil), postHook(&c, "A", nil))
			if got := runApp(t, app, tt.args...); got.code != tt.code {
				t.Errorf("ExecuteArgs(%q) = %d, stderr %q; want %d", tt.args, got.code, got.stderr, tt.code)
			}
			if got := c.String(); got != "" {
				t.Errorf("calls %q, want none", got)
			}
		})
	}
}

func TestAppHooksReceiveTheInvocationAndTheCommand(t *testing.T) {
	type seen struct {
		quiet       bool
		path        string
		annotations map[string]string
		args        []string
	}
	var pre, post seen
	record := func(s *seen, inv *spec.Invocation, cmd spec.CommandInfo) {
		*s = seen{quiet: inv.Output.Quiet(), path: cmd.String(), annotations: cmd.Annotations(), args: inv.Args}
	}
	app := cli.New(identity(), cli.WithQuiet(),
		cli.WithPreRun(func(_ context.Context, inv *spec.Invocation, cmd spec.CommandInfo) error {
			record(&pre, inv, cmd)
			cmd.Annotations()["k"] = "changed"
			return nil
		}),
		cli.WithPostRun(func(_ context.Context, inv *spec.Invocation, cmd spec.CommandInfo, _ error) error {
			record(&post, inv, cmd)
			return nil
		}),
	)
	ok := func(context.Context, *spec.Invocation) error { return nil }
	app.Add(&spec.Command{
		Name:        "remote",
		Summary:     "Manage remotes",
		Annotations: map[string]string{"parent": "only"},
		Commands: []*spec.Command{{
			Name:        "add",
			Summary:     "Add a remote",
			Args:        spec.ExactArgs(1),
			Annotations: map[string]string{"k": "v"},
			Run:         ok,
		}},
	})

	for range 2 {
		if got := runApp(t, app, "remote", "add", "-q", "origin"); got.code != cli.ExitOK {
			t.Fatalf("ExecuteArgs = %+v, want 0", got)
		}
		for name, s := range map[string]seen{"pre-run": pre, "post-run": post} {
			if !s.quiet {
				t.Errorf("%s hook: Output not quiet under -q", name)
			}
			if s.path != "tool remote add" {
				t.Errorf("%s hook: path %q, want %q", name, s.path, "tool remote add")
			}
			if len(s.annotations) != 1 || s.annotations["k"] != "v" {
				t.Errorf("%s hook: annotations %v, want only k=v", name, s.annotations)
			}
			if len(s.args) != 1 || s.args[0] != "origin" {
				t.Errorf("%s hook: args %q, want [origin]", name, s.args)
			}
		}
	}
}

// updateCheck returns a pre-run hook standing in for an update check: it records in c the
// commands it checks, and skips a command annotated with [spec.SkipUpdateCheck].
func updateCheck(c *calls) cli.Option {
	return cli.WithPreRun(func(_ context.Context, _ *spec.Invocation, cmd spec.CommandInfo) error {
		if _, skip := cmd.Annotation(spec.SkipUpdateCheck); skip {
			return nil
		}
		c.add("check %s", cmd)
		return nil
	})
}

func TestAppSkipUpdateCheckAnnotation(t *testing.T) {
	ok := func(context.Context, *spec.Invocation) error { return nil }
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "annotated command is skipped", args: []string{"self", "update"}},
		{
			name: "unannotated sibling under an annotated parent is checked",
			args: []string{"self", "status"},
			want: "check tool self status",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c calls
			app := cli.New(identity(), updateCheck(&c))
			app.Add(&spec.Command{
				Name:        "self",
				Summary:     "Manage the tool",
				Annotations: map[string]string{spec.SkipUpdateCheck: ""},
				Commands: []*spec.Command{
					{
						Name:        "update",
						Summary:     "Update the tool",
						Annotations: map[string]string{spec.SkipUpdateCheck: ""},
						Run:         ok,
					},
					{Name: "status", Summary: "Report the tool", Run: ok},
				},
			})
			if got := runApp(t, app, tt.args...); got.code != cli.ExitOK {
				t.Fatalf("ExecuteArgs(%q) = %+v, want 0", tt.args, got)
			}
			if got := c.String(); got != tt.want {
				t.Errorf("calls %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppHooksRunTwice(t *testing.T) {
	var c calls
	app := hookApp(&c, nil, preHook(&c, "A", nil), postHook(&c, "A", nil))
	for i := range 2 {
		if got := runApp(t, app, "remote", "add", "origin"); got.code != cli.ExitOK {
			t.Fatalf("run %d: %+v, want 0", i, got)
		}
	}
	want := "preA run postA(<nil>) preA run postA(<nil>)"
	if got := c.String(); got != want {
		t.Errorf("calls %q, want %q", got, want)
	}
}

func TestAppHooksConcurrent(t *testing.T) {
	app := cli.New(identity(),
		cli.WithPreRun(func(_ context.Context, inv *spec.Invocation, _ spec.CommandInfo) error {
			_, _ = fmt.Fprintf(inv.Env.Stdout, "pre %s;", inv.Args[0])
			return nil
		}),
		cli.WithPostRun(func(_ context.Context, inv *spec.Invocation, cmd spec.CommandInfo, _ error) error {
			_, _ = fmt.Fprintf(inv.Env.Stdout, "post %s %s", cmd, inv.Args[0])
			return nil
		}),
	)
	app.Add(&spec.Command{
		Name:    "echo",
		Summary: "Echo the argument",
		Args:    spec.ExactArgs(1),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, _ = fmt.Fprintf(inv.Env.Stdout, "run %s;", inv.Args[0])
			return nil
		},
	})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			name := fmt.Sprintf("n%d", i)
			var stdout bytes.Buffer
			code := app.ExecuteArgs(&spec.Env{Stdout: &stdout}, []string{"echo", name})
			want := fmt.Sprintf("pre %s;run %s;post tool echo %s", name, name, name)
			if code != cli.ExitOK || stdout.String() != want {
				t.Errorf("run %d: code %d, stdout %q; want 0 and %q", i, code, stdout.String(), want)
			}
		})
	}
	wg.Wait()
}

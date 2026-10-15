package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// newRoot builds the command tree of a tool named "tool", as its main package would.
func newRoot() *spec.Command {
	verbose := spec.BoolFlag("verbose", "print more", spec.Shorthand("v"), spec.Inherited())
	greeting := spec.StringFlag("greeting", "the word to greet with",
		spec.Shorthand("g"), spec.Default("hello"))
	times := spec.IntFlag("times", "how many times to greet", spec.Default(1))

	return &spec.Command{
		Name:    "tool",
		Summary: "A tool to test the executor",
		Flags:   []spec.Flag{verbose},
		Commands: []*spec.Command{
			{
				Name:    "greet",
				Summary: "Greet someone",
				Args:    spec.RangeArgs(0, 1),
				Flags:   []spec.Flag{greeting, times},
				Run: func(_ context.Context, inv *spec.Invocation) error {
					name := "world"
					if len(inv.Args) == 1 {
						name = inv.Args[0]
					}
					if verbose.Get(inv) {
						_, _ = fmt.Fprintln(inv.Env.Stdout, "verbose")
					}
					for range times.Get(inv) {
						_, _ = fmt.Fprintf(inv.Env.Stdout, "%s %s\n", greeting.Get(inv), name)
					}
					return nil
				},
			},
			{
				Name:    "fail",
				Summary: "Fail at runtime",
				Run: func(context.Context, *spec.Invocation) error {
					return errors.New("boom")
				},
			},
			{
				Name:    "env",
				Summary: "Print the environment",
				Run: func(_ context.Context, inv *spec.Invocation) error {
					in, err := io.ReadAll(inv.Env.Stdin)
					if err != nil {
						return err
					}
					token, ok := inv.Env.LookupEnv("TOOL_TOKEN")
					_, _ = fmt.Fprintf(inv.Env.Stdout, "workdir=%s token=%q set=%t stdin=%q\n",
						inv.Env.WorkDir, token, ok, in)
					_, _ = fmt.Fprintln(inv.Env.Stderr, "to stderr")
					return nil
				},
			},
			{
				Name:    "secret",
				Summary: "A hidden command",
				Hidden:  true,
				Run: func(_ context.Context, inv *spec.Invocation) error {
					_, _ = fmt.Fprintln(inv.Env.Stdout, "secret ran")
					return nil
				},
			},
			{
				Name:    "remote",
				Summary: "Manage remotes",
				Commands: []*spec.Command{
					{
						Name:    "add",
						Summary: "Add a remote",
						Args:    spec.ExactArgs(2),
						Run: func(_ context.Context, inv *spec.Invocation) error {
							_, _ = fmt.Fprintf(inv.Env.Stdout, "added %s %s verbose=%t\n",
								inv.Args[0], inv.Args[1], verbose.Get(inv))
							return nil
						},
					},
				},
			},
		},
	}
}

type result struct {
	code   int
	stdout string
	stderr string
}

// run executes newRoot with args against buffers, a fixed environment and a temporary working
// directory.
func run(t *testing.T, args ...string) result {
	t.Helper()
	return runTree(t, newRoot(), args...)
}

func runTree(t *testing.T, root *spec.Command, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	vars := map[string]string{"TOOL_TOKEN": "s3cret"}
	env := &spec.Env{
		Stdin:   strings.NewReader("input"),
		Stdout:  &stdout,
		Stderr:  &stderr,
		WorkDir: t.TempDir(),
		LookupEnv: func(key string) (string, bool) {
			v, ok := vars[key]
			return v, ok
		},
	}
	code := cli.ExecuteArgs(env, root, args)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestExecuteArgsExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want result
	}{
		{
			name: "success",
			args: []string{"greet", "ada"},
			want: result{code: cli.ExitOK, stdout: "hello ada\n"},
		},
		{
			name: "flags by name, shorthand and inherited",
			args: []string{"greet", "-v", "--times", "2", "-g", "hi", "ada"},
			want: result{code: cli.ExitOK, stdout: "verbose\nhi ada\nhi ada\n"},
		},
		{
			name: "inherited flag before the subcommand",
			args: []string{"--verbose", "greet"},
			want: result{code: cli.ExitOK, stdout: "verbose\nhello world\n"},
		},
		{
			name: "nested command",
			args: []string{"remote", "add", "-v", "origin", "https://example.com"},
			want: result{code: cli.ExitOK, stdout: "added origin https://example.com verbose=true\n"},
		},
		{
			name: "hidden command runs",
			args: []string{"secret"},
			want: result{code: cli.ExitOK, stdout: "secret ran\n"},
		},
		{
			name: "runtime error",
			args: []string{"fail"},
			want: result{code: cli.ExitError, stderr: "tool: boom\n"},
		},
		{
			name: "unknown flag",
			args: []string{"greet", "--bogus"},
			want: result{code: cli.ExitUsage, stderr: "tool: greet: unknown flag: --bogus\n"},
		},
		{
			name: "unknown shorthand flag",
			args: []string{"remote", "add", "-x", "a", "b"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: remote add: unknown shorthand flag: 'x' in -x\n"},
		},
		{
			name: "flag value that does not parse",
			args: []string{"greet", "--times", "many"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: greet: invalid argument \"many\" for \"--times\" flag: " +
					"strconv.ParseInt: parsing \"many\": invalid syntax\n"},
		},
		{
			name: "flag of a sibling command",
			args: []string{"fail", "--times", "2"},
			want: result{code: cli.ExitUsage, stderr: "tool: fail: unknown flag: --times\n"},
		},
		{
			name: "unknown subcommand",
			args: []string{"bogus"},
			want: result{code: cli.ExitUsage, stderr: "tool: unknown command \"bogus\" for \"tool\"\n"},
		},
		{
			name: "unknown nested subcommand",
			args: []string{"remote", "bogus"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: remote: unknown command \"bogus\" for \"tool remote\"\n"},
		},
		{
			name: "help is not a subcommand",
			args: []string{"help"},
			want: result{code: cli.ExitUsage, stderr: "tool: unknown command \"help\" for \"tool\"\n"},
		},
		{
			name: "completion is not a subcommand",
			args: []string{"completion"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: unknown command \"completion\" for \"tool\"\n"},
		},
		{
			name: "too few arguments",
			args: []string{"remote", "add", "origin"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: remote add: accepts exactly 2 arguments, received 1 argument\n"},
		},
		{
			name: "too many arguments",
			args: []string{"greet", "ada", "bob"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: greet: accepts between 0 and 1 arguments, received 2 arguments\n"},
		},
		{
			name: "argument to a command that takes none",
			args: []string{"fail", "now"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: fail: accepts no arguments, received 1 argument\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(t, tt.args...); got != tt.want {
				t.Errorf("ExecuteArgs(%q)\n got: %+v\nwant: %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestExecuteArgsHelp(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		notWant []string
	}{
		{
			name:    "root --help",
			args:    []string{"--help"},
			want:    []string{"A tool to test the executor", "greet", "Greet someone", "remote", "--verbose"},
			notWant: []string{"secret", "completion", "help [command]"},
		},
		{
			name: "root -h",
			args: []string{"-h"},
			want: []string{"greet", "remote"},
		},
		{
			name: "bare command with subcommands",
			args: nil,
			want: []string{"greet", "remote"},
		},
		{
			name: "subcommand --help",
			args: []string{"greet", "--help"},
			want: []string{"Greet someone", "--greeting", "-g", "--times", "--verbose"},
		},
		{
			name: "--help with a bad argument count",
			args: []string{"remote", "add", "--help"},
			want: []string{"Add a remote", "tool remote add"},
		},
		{
			name: "bare nested command with subcommands",
			args: []string{"remote"},
			want: []string{"Manage remotes", "add", "Add a remote"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := run(t, tt.args...)
			if got.code != cli.ExitOK || got.stderr != "" {
				t.Fatalf("ExecuteArgs(%q) = %d, stderr %q; want 0 and no stderr", tt.args, got.code, got.stderr)
			}
			for _, s := range tt.want {
				if !strings.Contains(got.stdout, s) {
					t.Errorf("help lacks %q:\n%s", s, got.stdout)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(got.stdout, s) {
					t.Errorf("help contains %q:\n%s", s, got.stdout)
				}
			}
		})
	}
}

func TestExecuteArgsInvalidTree(t *testing.T) {
	t.Run("invalid command", func(t *testing.T) {
		root := newRoot()
		root.Commands = append(root.Commands, &spec.Command{Name: "empty"})
		got := runTree(t, root, "greet")
		if got.code != cli.ExitError || got.stdout != "" {
			t.Errorf("code %d, stdout %q; want 1 and no stdout", got.code, got.stdout)
		}
		want := "tool: invalid command tree: " + root.Validate().Error() + "\n"
		if got.stderr != want {
			t.Errorf("stderr %q, want %q", got.stderr, want)
		}
	})
	t.Run("nil command", func(t *testing.T) {
		got := runTree(t, nil, "greet")
		if got.code != cli.ExitError || got.stdout != "" {
			t.Errorf("code %d, stdout %q; want 1 and no stdout", got.code, got.stdout)
		}
		if !strings.HasPrefix(got.stderr, "invalid command tree: ") {
			t.Errorf("stderr %q, want the prefix %q", got.stderr, "invalid command tree: ")
		}
	})
}

// probe returns a tree whose root reports what its Env gives it through the returned pointer.
func probe(seen *string) *spec.Command {
	return &spec.Command{
		Name: "probe",
		Args: spec.AnyArgs(),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			in, err := io.ReadAll(inv.Env.Stdin)
			if err != nil {
				return err
			}
			token, ok := inv.Env.LookupEnv("TOOL_TOKEN")
			*seen = fmt.Sprintf("token=%q set=%t stdin=%q args=%q", token, ok, in, inv.Args)
			_, _ = fmt.Fprintln(inv.Env.Stdout, "to stdout")
			_, _ = fmt.Fprintln(inv.Env.Stderr, "to stderr")
			return nil
		},
	}
}

func TestExecuteArgsNilEnv(t *testing.T) {
	t.Setenv("TOOL_TOKEN", "from the process")
	var seen string
	if code := cli.ExecuteArgs(nil, probe(&seen), nil); code != cli.ExitOK {
		t.Fatalf("code %d, want 0", code)
	}
	if want := `token="" set=false stdin="" args=[]`; seen != want {
		t.Errorf("run saw %s, want %s", seen, want)
	}
}

func TestExecuteArgsNilFields(t *testing.T) {
	t.Setenv("TOOL_TOKEN", "from the process")
	var seen string
	var stdout bytes.Buffer
	env := &spec.Env{Stdout: &stdout}
	if code := cli.ExecuteArgs(env, probe(&seen), []string{"a"}); code != cli.ExitOK {
		t.Fatalf("code %d, want 0", code)
	}
	if want := `token="" set=false stdin="" args=["a"]`; seen != want {
		t.Errorf("run saw %s, want %s", seen, want)
	}
	if stdout.String() != "to stdout\n" {
		t.Errorf("stdout %q, want %q", stdout.String(), "to stdout\n")
	}
}

func TestExecuteArgsEnv(t *testing.T) {
	var stdout, stderr bytes.Buffer
	dir := t.TempDir()
	env := &spec.Env{
		Stdin:     strings.NewReader("input"),
		Stdout:    &stdout,
		Stderr:    &stderr,
		WorkDir:   dir,
		LookupEnv: func(key string) (string, bool) { return "v-" + key, true },
	}
	if code := cli.ExecuteArgs(env, newRoot(), []string{"env"}); code != cli.ExitOK {
		t.Fatalf("code %d, want 0; stderr %q", code, stderr.String())
	}
	want := fmt.Sprintf("workdir=%s token=%q set=true stdin=%q\n", dir, "v-TOOL_TOKEN", "input")
	if stdout.String() != want {
		t.Errorf("stdout %q, want %q", stdout.String(), want)
	}
	if stderr.String() != "to stderr\n" {
		t.Errorf("stderr %q, want %q", stderr.String(), "to stderr\n")
	}
}

func TestExecuteArgsTreeRunsTwice(t *testing.T) {
	root := newRoot()
	first := runTree(t, root, "greet", "-v", "-g", "hey", "--times", "2", "ada")
	if want := "verbose\nhey ada\nhey ada\n"; first.code != cli.ExitOK || first.stdout != want {
		t.Fatalf("first run: %+v, want stdout %q", first, want)
	}
	second := runTree(t, root, "greet", "bob")
	if want := "hello bob\n"; second.code != cli.ExitOK || second.stdout != want {
		t.Errorf("second run: %+v, want stdout %q", second, want)
	}
}

func TestExecuteArgsConcurrent(t *testing.T) {
	root := newRoot()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			name := fmt.Sprintf("n%d", i)
			var stdout bytes.Buffer
			env := &spec.Env{Stdout: &stdout}
			code := cli.ExecuteArgs(env, root, []string{"greet", "--times", "1", "-g", name, name})
			if want := name + " " + name + "\n"; code != cli.ExitOK || stdout.String() != want {
				t.Errorf("run %d: code %d, stdout %q; want 0 and %q", i, code, stdout.String(), want)
			}
		})
	}
	wg.Wait()
}

// The test binary runs itself as a tool whose main calls cli.Execute when this variable is
// set, so Execute reads a real process's arguments, streams, directory and environment.
const executeChild = "CLI_CORE_EXECUTE_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(executeChild) {
	case "run":
		os.Args = append([]string{os.Args[0]}, strings.Fields(os.Getenv("CLI_CORE_EXECUTE_ARGS"))...)
		os.Exit(cli.Execute(newRoot()))
	case "nowd":
		dir := os.Getenv("CLI_CORE_EXECUTE_DIR")
		if err := os.Chdir(dir); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		if err := os.Remove(dir); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		os.Args = []string{os.Args[0], "greet"}
		os.Exit(cli.Execute(newRoot()))
	}
	os.Exit(m.Run())
}

// runChild runs the test binary as the tool in dir, with mode selecting the main TestMain runs
// and extra added to its environment.
func runChild(t *testing.T, mode, dir string, extra ...string) result {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(exe)
	cmd.Env = append(append(os.Environ(), executeChild+"="+mode), extra...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("input")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("running the child: %v", err)
		}
		code = exit.ExitCode()
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestExecuteUsesTheProcess(t *testing.T) {
	t.Setenv("TOOL_TOKEN", "from the process")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got := runChild(t, "run", dir, "CLI_CORE_EXECUTE_ARGS=env")
	want := result{
		code:   cli.ExitOK,
		stdout: fmt.Sprintf("workdir=%s token=%q set=true stdin=%q\n", dir, "from the process", "input"),
		stderr: "to stderr\n",
	}
	if got != want {
		t.Errorf("child\n got: %+v\nwant: %+v", got, want)
	}

	got = runChild(t, "run", dir, "CLI_CORE_EXECUTE_ARGS=greet --bogus")
	want = result{code: cli.ExitUsage, stderr: "tool: greet: unknown flag: --bogus\n"}
	if got != want {
		t.Errorf("child with a usage error\n got: %+v\nwant: %+v", got, want)
	}
}

func TestExecuteWithoutWorkingDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("removing the working directory of a process is reported by os.Getwd on Linux")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "gone")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := runChild(t, "nowd", parent, "CLI_CORE_EXECUTE_DIR="+dir)
	if got.code != cli.ExitError || got.stdout != "" || !strings.HasPrefix(got.stderr, "tool: ") {
		t.Errorf("child: %+v; want code 1, no stdout and a stderr starting %q", got, "tool: ")
	}
}

func TestExecuteArgsHelpFlagShorthand(t *testing.T) {
	t.Run("help takes -h by default", func(t *testing.T) {
		got := run(t, "greet", "--help")
		if got.code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr %q)", got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, "-h, --help") {
			t.Errorf("help does not list -h, --help:\n%s", got.stdout)
		}
	})

	t.Run("a flag holding -h keeps it", func(t *testing.T) {
		host := spec.StringFlag("host", "the host to use", spec.Shorthand("h"))
		root := &spec.Command{
			Name:  "tool",
			Flags: []spec.Flag{host},
			Run: func(_ context.Context, inv *spec.Invocation) error {
				_, _ = fmt.Fprintf(inv.Env.Stdout, "host=%s\n", host.Get(inv))
				return nil
			},
		}

		got := runTree(t, root, "-h", "example.org")
		if got.code != 0 || got.stdout != "host=example.org\n" {
			t.Errorf("-h example.org: code %d, stdout %q, stderr %q; want the flag set", got.code, got.stdout, got.stderr)
		}

		got = runTree(t, root, "--help")
		if got.code != 0 {
			t.Fatalf("--help: exit code = %d, want 0 (stderr %q)", got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, "--host string") || strings.Contains(got.stdout, "-h, --help") {
			t.Errorf("--help output lists the help flag with -h or omits --host:\n%s", got.stdout)
		}
	})
}

// sentinelError is the error the exit-code tests register a code for.
type sentinelError struct{}

func (sentinelError) Error() string { return "sentinel" }

// registerSentinel is a registry matcher giving code to every error that is a sentinelError.
func registerSentinel(code int) func(error) (int, bool) {
	return func(err error) (int, bool) {
		if errors.Is(err, sentinelError{}) {
			return code, true
		}
		return 0, false
	}
}

// returning returns a tree of a tool named "tool" whose command "do" returns what run returns
// given the context it receives.
func returning(run func(ctx context.Context) error) *spec.Command {
	return &spec.Command{
		Name: "tool",
		Commands: []*spec.Command{{
			Name: "do",
			Run: func(ctx context.Context, _ *spec.Invocation) error {
				return run(ctx)
			},
		}},
	}
}

// fails returns a tree whose command "do" returns err.
func fails(err error) *spec.Command {
	return returning(func(context.Context) error { return err })
}

// runCtx executes root with "do" against buffers, with ctx and c.
func runCtx(ctx context.Context, root *spec.Command, c cli.ExecuteConfig, args ...string) result {
	if args == nil {
		args = []string{"do"}
	}
	var stdout, stderr bytes.Buffer
	env := &spec.Env{Stdout: &stdout, Stderr: &stderr}
	code := cli.ExecuteArgsContext(ctx, env, root, args, c)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// cancelled returns a context that is already cancelled.
func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestExecuteArgsContextExitCodePrecedence(t *testing.T) {
	boom := errors.New("boom")
	usage := cli.NewUsageError(errors.New("bad usage"))
	registry := cli.ExecuteConfig{Registry: []func(error) (int, bool){registerSentinel(9)}}
	tests := []struct {
		name string
		ctx  context.Context
		root *spec.Command
		args []string
		c    cli.ExecuteConfig
		want result
	}{
		{
			name: "cancelled context, run returns its error",
			ctx:  cancelled(),
			root: returning(func(ctx context.Context) error { return ctx.Err() }),
			want: result{code: cli.ExitInterrupted},
		},
		{
			name: "cancelled context, run wraps its error",
			ctx:  cancelled(),
			root: returning(func(ctx context.Context) error { return fmt.Errorf("waiting: %w", ctx.Err()) }),
			want: result{code: cli.ExitInterrupted},
		},
		{
			name: "cancelled context outranks a coded error",
			ctx:  cancelled(),
			root: returning(func(ctx context.Context) error { return spec.Coded(5, ctx.Err()) }),
			c:    registry,
			want: result{code: cli.ExitInterrupted},
		},
		{
			name: "cancelled context, run succeeds",
			ctx:  cancelled(),
			root: fails(nil),
			want: result{code: cli.ExitOK},
		},
		{
			name: "cancelled context, another error is printed and coded",
			ctx:  cancelled(),
			root: fails(spec.Coded(7, boom)),
			want: result{code: 7, stderr: "tool: boom\n"},
		},
		{
			name: "cancelled context, another error is printed and registered",
			ctx:  cancelled(),
			root: fails(sentinelError{}),
			c:    registry,
			want: result{code: 9, stderr: "tool: sentinel\n"},
		},
		{
			name: "cancelled context, another error is printed",
			ctx:  cancelled(),
			root: fails(boom),
			want: result{code: cli.ExitError, stderr: "tool: boom\n"},
		},
		{
			name: "run's own timeout is not an interrupt",
			ctx:  context.Background(),
			root: returning(func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 0)
				defer cancel()
				<-ctx.Done()
				return ctx.Err()
			}),
			want: result{code: cli.ExitError, stderr: "tool: context deadline exceeded\n"},
		},
		{
			name: "run's own cancelled context is not an interrupt",
			ctx:  context.Background(),
			root: returning(func(ctx context.Context) error {
				ctx, cancel := context.WithCancel(ctx)
				cancel()
				return ctx.Err()
			}),
			want: result{code: cli.ExitError, stderr: "tool: context canceled\n"},
		},
		{
			name: "coded",
			ctx:  context.Background(),
			root: fails(fmt.Errorf("wrapped: %w", spec.Coded(7, boom))),
			want: result{code: 7, stderr: "tool: wrapped: boom\n"},
		},
		{
			name: "coded outranks the registry",
			ctx:  context.Background(),
			root: fails(spec.Coded(7, sentinelError{})),
			c:    registry,
			want: result{code: 7, stderr: "tool: sentinel\n"},
		},
		{
			name: "coded outranks usage",
			ctx:  context.Background(),
			root: fails(spec.Coded(7, usage)),
			want: result{code: 7, stderr: "tool: bad usage\n"},
		},
		{
			name: "registry",
			ctx:  context.Background(),
			root: fails(fmt.Errorf("wrapped: %w", sentinelError{})),
			c:    registry,
			want: result{code: 9, stderr: "tool: wrapped: sentinel\n"},
		},
		{
			name: "registry outranks usage",
			ctx:  context.Background(),
			root: fails(cli.NewUsageError(sentinelError{})),
			c:    registry,
			want: result{code: 9, stderr: "tool: sentinel\n"},
		},
		{
			name: "registry matches a command line the tree rejects",
			ctx:  context.Background(),
			root: fails(nil),
			args: []string{"do", "extra"},
			c: cli.ExecuteConfig{Registry: []func(error) (int, bool){func(err error) (int, bool) {
				var count *spec.ArgCountError
				return 11, errors.As(err, &count)
			}}},
			want: result{code: 11, stderr: "tool: do: accepts no arguments, received 1 argument\n"},
		},
		{
			name: "registry without a match",
			ctx:  context.Background(),
			root: fails(boom),
			c:    registry,
			want: result{code: cli.ExitError, stderr: "tool: boom\n"},
		},
		{
			name: "usage",
			ctx:  context.Background(),
			root: fails(usage),
			c:    registry,
			want: result{code: cli.ExitUsage, stderr: "tool: bad usage\n"},
		},
		{
			name: "reported",
			ctx:  context.Background(),
			root: fails(spec.Reported(boom)),
			want: result{code: cli.ExitError},
		},
		{
			name: "reported keeps its coded code",
			ctx:  context.Background(),
			root: fails(spec.Reported(spec.Coded(7, boom))),
			want: result{code: 7},
		},
		{
			name: "coded and reported in either order",
			ctx:  context.Background(),
			root: fails(spec.Coded(7, spec.Reported(boom))),
			want: result{code: 7},
		},
		{
			name: "reported keeps its registered code",
			ctx:  context.Background(),
			root: fails(spec.Reported(sentinelError{})),
			c:    registry,
			want: result{code: 9},
		},
		{
			name: "reported keeps its usage code",
			ctx:  context.Background(),
			root: fails(spec.Reported(usage)),
			want: result{code: cli.ExitUsage},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runCtx(tt.ctx, tt.root, tt.c, tt.args...); got != tt.want {
				t.Errorf("got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestExecuteArgsContextIgnoresUnregistrableCodes(t *testing.T) {
	for _, code := range []int{-1, 0, cli.ExitUsage, cli.ExitInterrupted, 126, 200} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			registry := cli.ExecuteConfig{Registry: []func(error) (int, bool){registerSentinel(9)}}
			tests := []struct {
				name string
				err  error
				c    cli.ExecuteConfig
				want int
			}{
				{name: "to the registry", err: spec.Coded(code, sentinelError{}), c: registry, want: 9},
				{name: "to usage", err: spec.Coded(code, cli.NewUsageError(errors.New("bad"))),
					want: cli.ExitUsage},
				{name: "to error", err: spec.Coded(code, errors.New("bad")), want: cli.ExitError},
			}
			for _, tt := range tests {
				got := runCtx(context.Background(), fails(tt.err), tt.c)
				if got.code != tt.want || got.stderr == "" {
					t.Errorf("%s: got %+v, want code %d and a message", tt.name, got, tt.want)
				}
			}
		})
	}
}

func TestExecuteArgsContextReachesTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), sentinelError{}, "value"))
	cancel()
	var gotValue any
	var gotErr error
	root := returning(func(ctx context.Context) error {
		gotValue, gotErr = ctx.Value(sentinelError{}), ctx.Err()
		return nil
	})
	if got := runCtx(ctx, root, cli.ExecuteConfig{}); got.code != cli.ExitOK {
		t.Fatalf("got %+v", got)
	}
	if gotValue != "value" || !errors.Is(gotErr, context.Canceled) {
		t.Errorf("run received a context with value %v and error %v", gotValue, gotErr)
	}
}

func TestExecuteArgsContextHooks(t *testing.T) {
	registry := []func(error) (int, bool){registerSentinel(9)}
	pre := func(err error) []func(context.Context, *spec.Invocation, spec.CommandInfo) error {
		return []func(context.Context, *spec.Invocation, spec.CommandInfo) error{
			func(context.Context, *spec.Invocation, spec.CommandInfo) error { return err },
		}
	}
	post := func(err error) []func(context.Context, *spec.Invocation, spec.CommandInfo, error) error {
		return []func(context.Context, *spec.Invocation, spec.CommandInfo, error) error{
			func(context.Context, *spec.Invocation, spec.CommandInfo, error) error { return err },
		}
	}
	tests := []struct {
		name string
		ctx  context.Context
		run  error
		c    cli.ExecuteConfig
		want result
	}{
		{
			name: "pre-run error is registered",
			ctx:  context.Background(),
			c:    cli.ExecuteConfig{Registry: registry, PreRun: pre(fmt.Errorf("pre: %w", sentinelError{}))},
			want: result{code: 9, stderr: "tool: pre: sentinel\n"},
		},
		{
			name: "pre-run error is coded",
			ctx:  context.Background(),
			c:    cli.ExecuteConfig{PreRun: pre(spec.Coded(7, errors.New("pre")))},
			want: result{code: 7, stderr: "tool: pre\n"},
		},
		{
			name: "pre-run error is an interrupt",
			ctx:  cancelled(),
			c:    cli.ExecuteConfig{PreRun: pre(context.Canceled)},
			want: result{code: cli.ExitInterrupted},
		},
		{
			name: "run error outranks a post-run error",
			ctx:  context.Background(),
			run:  spec.Coded(7, errors.New("run")),
			c:    cli.ExecuteConfig{Registry: registry, PostRun: post(sentinelError{})},
			want: result{code: 7, stderr: "tool: run\n"},
		},
		{
			name: "plain run error outranks a registered post-run error",
			ctx:  context.Background(),
			run:  errors.New("run"),
			c:    cli.ExecuteConfig{Registry: registry, PostRun: post(sentinelError{})},
			want: result{code: cli.ExitError, stderr: "tool: run\n"},
		},
		{
			name: "post-run error alone is an error",
			ctx:  context.Background(),
			c: cli.ExecuteConfig{
				Registry: registry,
				PostRun:  post(spec.Coded(7, fmt.Errorf("post: %w", sentinelError{}))),
			},
			want: result{code: cli.ExitError, stderr: "tool: post: sentinel\n"},
		},
		{
			name: "post-run usage error alone is an error",
			ctx:  context.Background(),
			c:    cli.ExecuteConfig{PostRun: post(cli.NewUsageError(errors.New("post")))},
			want: result{code: cli.ExitError, stderr: "tool: post\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runCtx(tt.ctx, fails(tt.run), tt.c); got != tt.want {
				t.Errorf("got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestExecuteArgsContextInvalidTreeIsPrinted(t *testing.T) {
	root := &spec.Command{Name: "tool", Commands: []*spec.Command{{Name: "x"}, {Name: "x"}}}
	c := cli.ExecuteConfig{Registry: []func(error) (int, bool){func(error) (int, bool) { return 9, true }}}
	got := runCtx(cancelled(), root, c)
	if got.code != cli.ExitError || !strings.HasPrefix(got.stderr, "tool: invalid command tree: ") {
		t.Errorf("got %+v, want ExitError and the invalid tree written", got)
	}
}

func TestExecuteArgsCodedAndReported(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want result
	}{
		{name: "coded", err: spec.Coded(7, boom), want: result{code: 7, stderr: "tool: boom\n"}},
		{name: "unregistrable code", err: spec.Coded(cli.ExitInterrupted, boom),
			want: result{code: cli.ExitError, stderr: "tool: boom\n"}},
		{name: "reported", err: spec.Reported(boom), want: result{code: cli.ExitError}},
		{name: "reported and coded", err: spec.Reported(spec.Coded(7, boom)), want: result{code: 7}},
		{name: "context canceled is not an interrupt", err: context.Canceled,
			want: result{code: cli.ExitError, stderr: "tool: context canceled\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runTree(t, fails(tt.err), "do"); got != tt.want {
				t.Errorf("got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

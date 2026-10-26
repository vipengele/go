package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
)

// identity returns the identity of a tool named "tool".
func identity() spec.Identity {
	return spec.Identity{
		Name:      "tool",
		Owner:     "vipengele",
		Repo:      "tool",
		EnvPrefix: "TOOL",
		DirName:   "tool",
	}
}

// newApp builds an App configured by opts holding the subcommands of newRoot, as a tool's main
// would. The root's inherited --verbose stays on newRoot, so the App's commands do not accept it.
func newApp(opts ...cli.Option) *cli.App {
	app := cli.New(identity(), opts...)
	for _, cmd := range newRoot().Commands {
		app.Add(cmd)
	}
	return app
}

func runApp(t *testing.T, app *cli.App, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := &spec.Env{Stdout: &stdout, Stderr: &stderr, WorkDir: t.TempDir()}
	code := app.ExecuteArgs(env, args)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestAppExecuteArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want result
	}{
		{
			name: "added command",
			args: []string{"greet", "ada"},
			want: result{code: cli.ExitOK, stdout: "hello ada\n"},
		},
		{
			name: "nested added command",
			args: []string{"remote", "add", "origin", "https://example.com"},
			want: result{code: cli.ExitOK, stdout: "added origin https://example.com verbose=false\n"},
		},
		{
			name: "run error is prefixed by the identity's name",
			args: []string{"fail"},
			want: result{code: cli.ExitError, stderr: "tool: boom\n"},
		},
		{
			name: "unknown command",
			args: []string{"bogus"},
			want: result{code: cli.ExitUsage, stderr: "tool: unknown command \"bogus\" for \"tool\"\n"},
		},
		{
			name: "argument count",
			args: []string{"greet", "ada", "bob"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: greet: accepts between 0 and 1 arguments, received 2 arguments\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runApp(t, newApp(), tt.args...); got != tt.want {
				t.Errorf("ExecuteArgs(%q)\n got: %+v\nwant: %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestAppExecuteArgsHelp(t *testing.T) {
	tests := []struct {
		name    string
		opts    []cli.Option
		args    []string
		want    []string
		notWant []string
	}{
		{
			name:    "root --help",
			args:    []string{"--help"},
			want:    []string{"tool [command]", "greet", "Greet someone", "remote", "Manage remotes"},
			notWant: []string{"secret", "completion", "help [command]"},
		},
		{
			name:    "root --help with completion",
			opts:    []cli.Option{cli.WithCompletion()},
			args:    []string{"--help"},
			want:    []string{"tool [command]", "greet", "remote", "completion"},
			notWant: []string{"secret", "help [command]"},
		},
		{
			name:    "completion --help",
			opts:    []cli.Option{cli.WithCompletion()},
			args:    []string{"completion", "--help"},
			want:    []string{"tool completion [command]", "bash", "zsh", "fish", "powershell"},
			notWant: []string{"greet"},
		},
		{
			name: "no arguments",
			args: nil,
			want: []string{"greet", "remote"},
		},
		{
			name: "added command --help",
			args: []string{"greet", "--help"},
			want: []string{"Greet someone", "tool greet", "--greeting", "--times"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runApp(t, newApp(tt.opts...), tt.args...)
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

func TestAppExecuteArgsInvalidTree(t *testing.T) {
	ok := func(context.Context, *spec.Invocation) error { return nil }
	tests := []struct {
		name  string
		build func() *cli.App
		want  string
	}{
		{
			name: "duplicate command name",
			build: func() *cli.App {
				app := cli.New(identity())
				app.Add(&spec.Command{Name: "dup", Run: ok})
				app.Add(&spec.Command{Name: "dup", Run: ok})
				return app
			},
			want: "tool: invalid command tree\n",
		},
		{
			name: "invalid command",
			build: func() *cli.App {
				app := cli.New(identity())
				app.Add(&spec.Command{Name: "empty"})
				return app
			},
			want: "tool: invalid command tree\n",
		},
		{
			name: "nil command",
			build: func() *cli.App {
				app := cli.New(identity())
				app.Add(nil)
				return app
			},
			want: "tool: invalid command tree\n",
		},
		{
			name: "added command named version",
			build: func() *cli.App {
				app := cli.New(identity())
				app.Add(&spec.Command{Name: "version", Run: ok})
				return app
			},
			want: "tool: invalid command tree\n",
		},
		{
			name: "identity without a name",
			build: func() *cli.App {
				app := cli.New(spec.Identity{})
				app.Add(&spec.Command{Name: "run", Run: ok})
				return app
			},
			want: "invalid command tree\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runApp(t, tt.build(), "dup")
			if got.code != cli.ExitError || got.stdout != "" {
				t.Errorf("code %d, stdout %q; want 1 and no stdout", got.code, got.stdout)
			}
			if !strings.HasPrefix(got.stderr, tt.want) {
				t.Errorf("stderr %q, want the prefix %q", got.stderr, tt.want)
			}
		})
	}
}

func TestAppAddKeepsOrderAndLeavesCommandsUnchanged(t *testing.T) {
	ok := func(context.Context, *spec.Invocation) error { return nil }
	zeta := &spec.Command{Name: "zeta", Summary: "Last added first", Run: ok}
	alpha := &spec.Command{Name: "alpha", Summary: "First added last", Run: ok}
	app := cli.New(identity())
	app.Add(zeta)
	app.Add(alpha)

	got := runApp(t, app, "--help")
	if got.code != cli.ExitOK {
		t.Fatalf("code %d, stderr %q; want 0", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "zeta") || !strings.Contains(got.stdout, "alpha") {
		t.Errorf("help lacks an added command:\n%s", got.stdout)
	}
	if zeta.Commands != nil || alpha.Commands != nil {
		t.Errorf("an added command gained subcommands: zeta %v, alpha %v", zeta.Commands, alpha.Commands)
	}
}

func TestAppRunsTwice(t *testing.T) {
	app := newApp()
	first := runApp(t, app, "greet", "-g", "hey", "--times", "2", "ada")
	if want := "hey ada\nhey ada\n"; first.code != cli.ExitOK || first.stdout != want {
		t.Fatalf("first run: %+v, want stdout %q", first, want)
	}
	second := runApp(t, app, "greet", "bob")
	if want := "hello bob\n"; second.code != cli.ExitOK || second.stdout != want {
		t.Errorf("second run: %+v, want stdout %q", second, want)
	}
}

func TestAppConcurrent(t *testing.T) {
	app := newApp()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			name := fmt.Sprintf("n%d", i)
			var stdout bytes.Buffer
			env := &spec.Env{Stdout: &stdout}
			code := app.ExecuteArgs(env, []string{"greet", "--times", "1", "-g", name, name})
			if want := name + " " + name + "\n"; code != cli.ExitOK || stdout.String() != want {
				t.Errorf("run %d: code %d, stdout %q; want 0 and %q", i, code, stdout.String(), want)
			}
		})
	}
	wg.Wait()
}

func TestAppInterruptCancelsTheContextOfHooksAndRun(t *testing.T) {
	baseline := runtime.NumGoroutine()
	preCtx := make(chan context.Context, 1)
	var runErr, postErr error
	app := cli.New(identity(),
		cli.WithPreRun(func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
			preCtx <- ctx
			<-ctx.Done()
			return ctx, nil
		}),
		cli.WithPostRun(func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo, _ error) error {
			postErr = ctx.Err()
			return errors.New("hook failed")
		}),
	)
	app.Add(&spec.Command{
		Name: "wait",
		Run: func(ctx context.Context, _ *spec.Invocation) error {
			runErr = ctx.Err()
			return nil
		},
	})

	ch := make(chan struct{})
	var stderr bytes.Buffer
	codes := make(chan int, 1)
	go func() {
		codes <- app.ExecuteArgs(&spec.Env{Stderr: &stderr, Interrupts: ch}, []string{"wait"})
	}()
	if err := (<-preCtx).Err(); err != nil {
		t.Fatalf("pre-run context before an interrupt: %v, want nil", err)
	}
	ch <- struct{}{}
	select {
	case code := <-codes:
		// The post-run error is opaque, so the interrupt leaves it the code it has without one.
		if code != cli.ExitError || stderr.String() != "tool: hook failed\n" {
			t.Errorf("code %d, stderr %q; want %d and %q",
				code, stderr.String(), cli.ExitError, "tool: hook failed\n")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ExecuteArgs did not return after the interrupt cancelled its context")
	}
	if !errors.Is(runErr, context.Canceled) || !errors.Is(postErr, context.Canceled) {
		t.Errorf("run saw %v, post-run hook saw %v; want %v for both", runErr, postErr, context.Canceled)
	}
	waitGoroutines(t, baseline)
}

func TestAppExecuteUsesTheProcess(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()
	args, stdout := os.Args, os.Stdout
	defer func() { os.Args, os.Stdout = args, stdout }()
	os.Args = []string{"tool", "greet", "ada"}
	os.Stdout = out

	code := newApp().Execute()

	os.Args, os.Stdout = args, stdout
	written, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if want := "hello ada\n"; code != cli.ExitOK || string(written) != want {
		t.Errorf("Execute = %d, stdout %q; want 0 and %q", code, written, want)
	}
}

// versionInfo is the build information the App of a version test prints.
func versionInfo() buildinfo.Info {
	return buildinfo.Info{
		Version:   "v1.2.3",
		Commit:    "0123abcdef0123abcdef",
		Date:      "2026-01-02T03:04:05Z",
		GoVersion: "go1.27.2",
		OS:        "linux",
		Arch:      "amd64",
	}
}

func TestAppVersion(t *testing.T) {
	const text = "v1.2.3 build 0123abcdef01\n"
	const asJSON = `{
  "schema": 1,
  "version": "v1.2.3",
  "commit": "0123abcdef0123abcdef",
  "date": "2026-01-02T03:04:05Z",
  "go": "go1.27.2",
  "os": "linux",
  "arch": "amd64",
  "dev": false
}
`
	tests := []struct {
		name string
		opts []cli.Option
		args []string
		want result
	}{
		{name: "text", args: []string{"version"}, want: result{code: cli.ExitOK, stdout: text}},
		{name: "local --json", args: []string{"version", "--json"}, want: result{code: cli.ExitOK, stdout: asJSON}},
		{
			name: "local --json is not the root's",
			args: []string{"--json", "version"},
			want: result{code: cli.ExitUsage, stderr: "tool: unknown flag: --json\n"},
		},
		{
			name: "text with the standard --json",
			opts: []cli.Option{cli.WithJSON()},
			args: []string{"version"},
			want: result{code: cli.ExitOK, stdout: text},
		},
		{
			name: "standard --json after version",
			opts: []cli.Option{cli.WithJSON()},
			args: []string{"version", "--json"},
			want: result{code: cli.ExitOK, stdout: asJSON},
		},
		{
			name: "standard --json before version",
			opts: []cli.Option{cli.WithJSON()},
			args: []string{"--json", "version"},
			want: result{code: cli.ExitOK, stdout: asJSON},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := cli.New(identity(), append([]cli.Option{cli.WithBuildInfo(versionInfo())}, tt.opts...)...)
			if got := runApp(t, app, tt.args...); got != tt.want {
				t.Errorf("ExecuteArgs(%q)\n got: %+v\nwant: %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestAppVersionWithoutAddedCommands(t *testing.T) {
	app := cli.New(identity(), cli.WithBuildInfo(versionInfo()))
	help := runApp(t, app, "--help")
	if help.code != cli.ExitOK || !strings.Contains(help.stdout, "version") {
		t.Errorf("--help = %+v, want 0 and a listing of version", help)
	}
	if got := runApp(t, app, "version"); got.code != cli.ExitOK || got.stdout != "v1.2.3 build 0123abcdef01\n" {
		t.Errorf("version = %+v, want 0 and the build information", got)
	}
}

func TestAppVersionSkipsUpdateCheck(t *testing.T) {
	var annotated bool
	app := cli.New(identity(), cli.WithBuildInfo(versionInfo()),
		cli.WithPreRun(func(ctx context.Context, _ *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
			_, annotated = cmd.Annotation(spec.SkipUpdateCheck)
			return ctx, nil
		}))
	if got := runApp(t, app, "version"); got.code != cli.ExitOK {
		t.Fatalf("version = %+v, want 0", got)
	}
	if !annotated {
		t.Errorf("version lacks the %q annotation", spec.SkipUpdateCheck)
	}
}

func TestAppVersionReadsTheBinary(t *testing.T) {
	got := runApp(t, cli.New(identity()), "version", "--json")
	if got.code != cli.ExitOK {
		t.Fatalf("version --json = %+v, want 0", got)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &m); err != nil {
		t.Fatalf("output %q is not JSON: %v", got.stdout, err)
	}
	if m["go"] != runtime.Version() {
		t.Errorf("go = %v, want %q", m["go"], runtime.Version())
	}
}

// exitErr is the error type the exit-code option tests register by type.
type exitErr struct{ code int }

func (e exitErr) Error() string { return fmt.Sprintf("exit %d", e.code) }

// exitSentinel is an error whose identity is its text, so a constant of it is a sentinel.
type exitSentinel string

func (e exitSentinel) Error() string { return string(e) }

const errExitSentinel = exitSentinel("sentinel")

// exitApp builds an App with opts whose command "do" returns err.
func exitApp(err error, opts ...cli.Option) *cli.App {
	app := cli.New(identity(), opts...)
	app.Add(&spec.Command{
		Name: "do",
		Run:  func(context.Context, *spec.Invocation) error { return err },
	})
	return app
}

func TestAppExitCodes(t *testing.T) {
	byType := cli.WithExitCodeAs(func(e exitErr) int { return e.code })
	tests := []struct {
		name string
		err  error
		opts []cli.Option
		want int
	}{
		{
			name: "sentinel",
			err:  fmt.Errorf("wrapped: %w", errExitSentinel),
			opts: []cli.Option{cli.WithExitCode(7, errExitSentinel)},
			want: 7,
		},
		{name: "typed error", err: exitErr{code: 9}, opts: []cli.Option{byType}, want: 9},
		{name: "typed error with an unregistrable code", err: exitErr{code: 130}, opts: []cli.Option{byType},
			want: cli.ExitError},
		{
			name: "unrelated error",
			err:  errors.New("other"),
			opts: []cli.Option{cli.WithExitCode(7, errExitSentinel), byType},
			want: cli.ExitError,
		},
		{
			name: "coded wins over a registration",
			err:  spec.Coded(4, errExitSentinel),
			opts: []cli.Option{cli.WithExitCode(7, errExitSentinel)},
			want: 4,
		},
		{
			name: "first registered wins, sentinel first",
			err:  fmt.Errorf("%w: %w", errExitSentinel, exitErr{code: 9}),
			opts: []cli.Option{cli.WithExitCode(7, errExitSentinel), byType},
			want: 7,
		},
		{
			name: "first registered wins, typed first",
			err:  fmt.Errorf("%w: %w", errExitSentinel, exitErr{code: 9}),
			opts: []cli.Option{byType, cli.WithExitCode(7, errExitSentinel)},
			want: 9,
		},
		{
			name: "run usage",
			err:  fmt.Errorf("deploy: %w", &spec.UsageError{Question: "continue?", Flag: "--yes"}),
			opts: []cli.Option{cli.WithExitCode(7, errExitSentinel), byType},
			want: cli.ExitUsage,
		},
		{
			name: "registration wins over run usage",
			err:  &spec.UsageError{Question: "continue?", Flag: "--yes"},
			opts: []cli.Option{cli.WithExitCodeAs(func(*spec.UsageError) int { return 6 })},
			want: 6,
		},
		{
			name: "coded wins over run usage",
			err:  spec.Coded(4, &spec.UsageError{Question: "continue?"}),
			opts: []cli.Option{cli.WithExitCodeAs(func(*spec.UsageError) int { return 6 })},
			want: 4,
		},
		{
			name: "nil fn is ignored",
			err:  exitErr{code: 9},
			opts: []cli.Option{cli.WithExitCodeAs[exitErr](nil)},
			want: cli.ExitError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runApp(t, exitApp(tt.err, tt.opts...), "do"); got.code != tt.want {
				t.Errorf("exit code %d, want %d (stderr %q)", got.code, tt.want, got.stderr)
			}
		})
	}
}

func TestAppExitCodesAreIndependentPerApp(t *testing.T) {
	a := exitApp(errExitSentinel, cli.WithExitCode(7, errExitSentinel))
	b := exitApp(errExitSentinel, cli.WithExitCode(8, errExitSentinel))
	c := exitApp(errExitSentinel)
	for _, tt := range []struct {
		app  *cli.App
		want int
	}{{a, 7}, {b, 8}, {c, cli.ExitError}, {a, 7}} {
		if got := runApp(t, tt.app, "do"); got.code != tt.want {
			t.Errorf("exit code %d, want %d", got.code, tt.want)
		}
	}
}

func TestAppErrorRenderer(t *testing.T) {
	byType := func(out string) cli.Option {
		return cli.WithErrorRenderer(func(e exitErr) string { return fmt.Sprintf(out, e.code) })
	}
	tests := []struct {
		name string
		err  error
		opts []cli.Option
		want string
	}{
		{
			name: "typed error",
			err:  exitErr{code: 9},
			opts: []cli.Option{byType("code %d")},
			want: "tool: code 9\n",
		},
		{
			name: "detail lines indent under the row",
			err:  exitErr{code: 9},
			opts: []cli.Option{byType("code %d\nretry later")},
			want: "tool: code 9\n  retry later\n",
		},
		{
			name: "typed error under a wrap",
			err:  fmt.Errorf("doing: %w", exitErr{code: 9}),
			opts: []cli.Option{byType("code %d")},
			want: "tool: doing\n  → code 9\n",
		},
		{
			name: "typed error in a join",
			err:  fmt.Errorf("doing: %w", errors.Join(errExitSentinel, exitErr{code: 9})),
			opts: []cli.Option{byType("code %d")},
			want: "tool: doing\n  • sentinel\n  • code 9\n",
		},
		{
			name: "the rendered error's subtree is replaced",
			err:  spec.Coded(4, fmt.Errorf("inner: %w", errExitSentinel)),
			opts: []cli.Option{cli.WithErrorRenderer(func(e *spec.CodedError) string {
				return fmt.Sprintf("code %d", e.Code())
			})},
			want: "tool: code 4\n",
		},
		{
			name: "other type is rendered by default",
			err:  fmt.Errorf("doing: %w", errExitSentinel),
			opts: []cli.Option{byType("code %d")},
			want: "tool: doing\n  → sentinel\n",
		},
		{
			name: "nil fn is ignored",
			err:  exitErr{code: 9},
			opts: []cli.Option{cli.WithErrorRenderer[exitErr](nil)},
			want: "tool: exit 9\n",
		},
		{
			name: "empty string renders by default",
			err:  exitErr{code: 9},
			opts: []cli.Option{cli.WithErrorRenderer(func(exitErr) string { return "" })},
			want: "tool: exit 9\n",
		},
		{
			name: "first registered wins",
			err:  exitErr{code: 9},
			opts: []cli.Option{byType("first %d"), byType("second %d")},
			want: "tool: first 9\n",
		},
		{
			name: "reported error is not written",
			err:  spec.Reported(exitErr{code: 9}),
			opts: []cli.Option{byType("code %d")},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runApp(t, exitApp(tt.err, tt.opts...), "do"); got.stderr != tt.want {
				t.Errorf("stderr %q, want %q", got.stderr, tt.want)
			}
		})
	}
}

func TestErrorRendererLeavesThePackageLevelPathAlone(t *testing.T) {
	err := fmt.Errorf("doing: %w", exitErr{code: 9})
	app := exitApp(err, cli.WithErrorRenderer(func(e exitErr) string { return "rendered" }))
	if got := runApp(t, app, "do"); got.stderr != "tool: doing\n  → rendered\n" {
		t.Fatalf("App stderr %q, want the renderer's output", got.stderr)
	}
	root := &spec.Command{
		Name:     "tool",
		Commands: []*spec.Command{{Name: "do", Run: func(context.Context, *spec.Invocation) error { return err }}},
	}
	var stderr bytes.Buffer
	cli.ExecuteArgs(&spec.Env{Stderr: &stderr, WorkDir: t.TempDir()}, root, []string{"do"})
	if want := "tool: doing\n  → exit 9\n"; stderr.String() != want {
		t.Errorf("ExecuteArgs stderr %q, want %q", stderr.String(), want)
	}
}

func TestWithExitCodePanicsInNewForAReservedCode(t *testing.T) {
	for _, code := range []int{0, 2, 130, -1, 126} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			defer func() {
				r := recover()
				msg, _ := r.(string)
				if want := fmt.Sprintf("exit code %d ", code); !strings.Contains(msg, want) {
					t.Errorf("panic %v, want a message containing %q", r, want)
				}
			}()
			cli.New(identity(), cli.WithExitCode(code, errExitSentinel))
		})
	}
}

func TestWithExitCodeAcceptsARegistrableCode(t *testing.T) {
	for _, code := range []int{1, 3, 125} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			app := exitApp(errExitSentinel, cli.WithExitCode(code, errExitSentinel))
			if got := runApp(t, app, "do"); got.code != code {
				t.Errorf("exit code %d, want %d", got.code, code)
			}
		})
	}
}

func TestAppCompletionNeedsTheOption(t *testing.T) {
	for _, args := range [][]string{{"completion"}, {"completion", "bash"}} {
		got := runApp(t, newApp(), args...)
		want := result{code: cli.ExitUsage, stderr: "tool: unknown command \"completion\" for \"tool\"\n"}
		if got != want {
			t.Errorf("ExecuteArgs(%q) without WithCompletion\n got: %+v\nwant: %+v", args, got, want)
		}
	}
}

func TestWithCompletionWritesEveryShellsScript(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			got := runApp(t, newApp(cli.WithCompletion()), "completion", shell)
			if got.code != cli.ExitOK || got.stderr != "" {
				t.Fatalf("completion %s = %d, stderr %q; want 0 and no stderr", shell, got.code, got.stderr)
			}
			if !strings.Contains(got.stdout, "__complete") {
				t.Errorf("completion %s wrote %q, want a script that calls __complete", shell, got.stdout)
			}
		})
	}
}

func TestWithCompletionRunsNoHook(t *testing.T) {
	var s seen
	called := false
	hook := func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
		called = true
		return ctx, errors.New("hook ran")
	}
	app := configApp(&s, cli.WithCompletion(), cli.WithPreRun(hook))
	got := newConfigEnv(t, "port: [1]\n").run(t, app, "completion", "bash")
	if got.code != cli.ExitOK || got.stderr != "" || got.stdout == "" {
		t.Errorf("completion bash with a broken config.yaml = %+v, want 0, a script and no stderr", got)
	}
	if called {
		t.Error("completion bash ran the pre-run hook")
	}
}

func TestWithCompletionKeepsAToolsOwnCompletionCommand(t *testing.T) {
	app := cli.New(identity(), cli.WithCompletion())
	app.Add(&spec.Command{
		Name:    "completion",
		Summary: "The tool's own completion",
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, _ = fmt.Fprintln(inv.Env.Stdout, "own")
			return nil
		},
	})
	if got, want := runApp(t, app, "completion"), (result{code: cli.ExitOK, stdout: "own\n"}); got != want {
		t.Errorf("completion\n got: %+v\nwant: %+v", got, want)
	}
}

func TestAppCompletesWithoutTheOptionAndRunsNoHook(t *testing.T) {
	var s seen
	called := false
	hook := func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
		called = true
		return ctx, errors.New("hook ran")
	}
	app := configApp(&s, cli.WithPreRun(hook))
	app.Add(&spec.Command{
		Name:          "pick",
		Summary:       "Pick a colour",
		Args:          spec.ExactArgs(1),
		ArgCompletion: spec.CompleteChoices("red", "green"),
		Run:           func(context.Context, *spec.Invocation) error { return nil },
	})
	got := newConfigEnv(t, "port: [1]\n").run(t, app, "__complete", "pick", "")
	if got.code != cli.ExitOK || got.stdout != "red\ngreen\n:4\n" {
		t.Errorf("__complete pick \"\" with a broken config.yaml = %+v, want 0 and red, green, :4 on stdout", got)
	}
	if strings.Contains(got.stderr, "load config") || called || s.called {
		t.Errorf("__complete ran a hook or a run function: stderr %q, hook %t, run %t", got.stderr, called, s.called)
	}
}

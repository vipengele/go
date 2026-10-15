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

// newApp builds an App holding the subcommands of newRoot, as a tool's main would. The root's
// inherited --verbose stays on newRoot, so the App's commands do not accept it.
func newApp() *cli.App {
	app := cli.New(identity())
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
			got := runApp(t, newApp(), tt.args...)
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
			want: "tool: invalid command tree: ",
		},
		{
			name: "invalid command",
			build: func() *cli.App {
				app := cli.New(identity())
				app.Add(&spec.Command{Name: "empty"})
				return app
			},
			want: "tool: invalid command tree: ",
		},
		{
			name: "nil command",
			build: func() *cli.App {
				app := cli.New(identity())
				app.Add(nil)
				return app
			},
			want: "tool: invalid command tree: ",
		},
		{
			name: "added command named version",
			build: func() *cli.App {
				app := cli.New(identity())
				app.Add(&spec.Command{Name: "version", Run: ok})
				return app
			},
			want: "tool: invalid command tree: ",
		},
		{
			name: "identity without a name",
			build: func() *cli.App {
				app := cli.New(spec.Identity{})
				app.Add(&spec.Command{Name: "run", Run: ok})
				return app
			},
			want: "invalid command tree: ",
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
		cli.WithPreRun(func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) error {
			preCtx <- ctx
			<-ctx.Done()
			return nil
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
		if code != cli.ExitInterrupted || stderr.String() != "tool: hook failed\n" {
			t.Errorf("code %d, stderr %q; want %d and %q",
				code, stderr.String(), cli.ExitInterrupted, "tool: hook failed\n")
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
	const asJSON = `{"schema":1,"version":"v1.2.3","commit":"0123abcdef0123abcdef",` +
		`"date":"2026-01-02T03:04:05Z","go":"go1.27.2","os":"linux","arch":"amd64","dev":false}` + "\n"
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
		cli.WithPreRun(func(_ context.Context, _ *spec.Invocation, cmd spec.CommandInfo) error {
			_, annotated = cmd.Annotation(spec.SkipUpdateCheck)
			return nil
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

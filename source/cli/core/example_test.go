package cli_test

import (
	"context"
	"fmt"
	"os"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
)

func Example() {
	loud := spec.BoolFlag("loud", "shout the greeting", spec.Shorthand("l"))
	greet := &spec.Command{
		Name:    "greet",
		Summary: "Greet someone",
		Args:    spec.ExactArgs(1),
		Flags:   []spec.Flag{loud},
		Run: func(_ context.Context, inv *spec.Invocation) error {
			greeting := "hello, " + inv.Args[0]
			if loud.Get(inv) {
				greeting += "!"
			}
			if inv.Output.JSON() {
				greeting = fmt.Sprintf("{%q:%q}", "greeting", greeting)
			}
			_, err := fmt.Fprintln(inv.Env.Stdout, greeting)
			return err
		},
	}
	offline := &spec.Command{
		Name:        "offline",
		Summary:     "Work without the network",
		Annotations: map[string]string{spec.SkipUpdateCheck: ""},
		Run:         func(context.Context, *spec.Invocation) error { return nil },
	}

	// A hook standing in for the update check a tool's main wires: it skips a command
	// annotated with spec.SkipUpdateCheck, "version" included.
	check := func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
		if _, skip := cmd.Annotation(spec.SkipUpdateCheck); !skip {
			_, _ = fmt.Fprintln(inv.Env.Stderr, "checking for updates before", cmd)
		}
		return ctx, nil
	}

	app := cli.New(spec.Identity{
		Name:      "hello",
		Owner:     "vipengele",
		Repo:      "hello",
		EnvPrefix: "HELLO",
		DirName:   "hello",
	}, cli.WithVerbose(), cli.WithJSON(), cli.WithPreRun(check),
		cli.WithBuildInfo(buildinfo.Info{Version: "v1.0.0", Commit: "0123abcdef0123abcdef"}))
	app.Add(greet)
	app.Add(offline)

	// A tool's main runs os.Exit(app.Execute()); ExecuteArgs runs the same commands against
	// the Env and arguments it is given.
	env := &spec.Env{Stdout: os.Stdout, Stderr: os.Stdout}
	fmt.Println("exit", app.ExecuteArgs(env, []string{"greet", "--loud", "world"}))
	fmt.Println("exit", app.ExecuteArgs(env, []string{"--json", "greet", "world"}))
	fmt.Println("exit", app.ExecuteArgs(env, []string{"greet", "world", "again"}))
	fmt.Println("exit", app.ExecuteArgs(env, []string{"offline"}))
	fmt.Println("exit", app.ExecuteArgs(env, []string{"version"}))
	// Output:
	// checking for updates before hello greet
	// hello, world!
	// exit 0
	// checking for updates before hello greet
	// {"greeting":"hello, world"}
	// exit 0
	// hello: greet: accepts exactly 1 argument, received 2 arguments
	// exit 2
	// exit 0
	// v1.0.0 build 0123abcdef01
	// exit 0
}

func ExampleExecuteArgs() {
	root := &spec.Command{
		Name: "hello",
		Args: spec.ExactArgs(1),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, err := fmt.Fprintln(inv.Env.Stdout, "hello, "+inv.Args[0])
			return err
		},
	}

	// A tool that builds its own root runs os.Exit(cli.Execute(root)) from main.
	env := &spec.Env{Stdout: os.Stdout, Stderr: os.Stdout}
	fmt.Println("exit", cli.ExecuteArgs(env, root, []string{"world"}))
	// Output:
	// hello, world
	// exit 0
}

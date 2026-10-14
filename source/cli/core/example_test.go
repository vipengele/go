package cli_test

import (
	"context"
	"fmt"
	"os"

	cli "go.vipengele.org/cli/core"
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
			_, err := fmt.Fprintln(inv.Env.Stdout, greeting)
			return err
		},
	}

	app := cli.New(spec.Identity{
		Name:      "hello",
		Owner:     "vipengele",
		Repo:      "hello",
		EnvPrefix: "HELLO",
		DirName:   "hello",
	})
	app.Add(greet)

	// A tool's main runs os.Exit(app.Execute()); ExecuteArgs runs the same commands against
	// the Env and arguments it is given.
	env := &spec.Env{Stdout: os.Stdout, Stderr: os.Stdout}
	fmt.Println("exit", app.ExecuteArgs(env, []string{"greet", "--loud", "world"}))
	fmt.Println("exit", app.ExecuteArgs(env, []string{"greet", "world", "again"}))
	// Output:
	// hello, world!
	// exit 0
	// hello: greet: accepts exactly 1 argument, received 2 arguments
	// exit 2
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

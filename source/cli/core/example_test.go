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
	root := &spec.Command{
		Name:    "hello",
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

	// A tool's main runs os.Exit(cli.Execute(root)); ExecuteArgs runs the same tree against
	// the Env and arguments it is given.
	env := &spec.Env{Stdout: os.Stdout, Stderr: os.Stdout}
	fmt.Println("exit", cli.ExecuteArgs(env, root, []string{"--loud", "world"}))
	fmt.Println("exit", cli.ExecuteArgs(env, root, []string{"world", "again"}))
	// Output:
	// hello, world!
	// exit 0
	// hello: accepts exactly 1 argument, received 2 arguments
	// exit 2
}

package spec_test

import (
	"context"
	"fmt"
	"strings"

	"go.vipengele.org/cli/spec"
)

func ExampleCommand() {
	verbose := spec.BoolFlag("verbose", "log more", spec.Shorthand("v"), spec.Inherited())
	format := spec.StringFlag("format", "output format", spec.Default("text"))

	status := &spec.Command{
		Name:    "status",
		Summary: "show the status",
		Args:    spec.RangeArgs(0, 1),
		Flags:   []spec.Flag{format},
		Run: func(_ context.Context, inv *spec.Invocation) error {
			_, err := fmt.Fprintf(inv.Env.Stdout, "format=%s verbose=%t args=%v\n",
				format.Get(inv), verbose.Get(inv), inv.Args)
			return err
		},
	}
	root := &spec.Command{
		Name:     "tool",
		Flags:    []spec.Flag{verbose},
		Commands: []*spec.Command{status},
	}
	if err := root.Validate(); err != nil {
		fmt.Println(err)
		return
	}

	// An adapter parses the command line "tool status -v web" into an invocation.
	args := []string{"web"}
	if err := status.Args.Check(args); err != nil {
		fmt.Println(err)
		return
	}
	var out strings.Builder
	inv := spec.NewInvocation(&spec.Env{Stdout: &out}, args, map[string]any{"verbose": true})
	if err := status.Run(context.Background(), inv); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(out.String())
	fmt.Println(status.Args.Check([]string{"a", "b"}))
	// Output:
	// format=text verbose=true args=[web]
	// accepts between 0 and 1 arguments, received 2 arguments
}

func ExampleIdentity() {
	id := spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
	if err := id.Validate(); err != nil {
		fmt.Println(err)
		return
	}

	noColor, _ := id.EnvVar("NO_COLOR")
	cache, _ := id.Subdir("cache")
	fmt.Println(id.Slug())
	fmt.Println(noColor)
	fmt.Println(cache)
	// Output:
	// pedromvgomes/agentic-toolkit
	// ATK_NO_COLOR
	// agentic-toolkit/cache
}

func ExampleOutput() {
	report := func(_ context.Context, inv *spec.Invocation) error {
		if inv.Output.JSON() {
			_, err := fmt.Fprintln(inv.Env.Stdout, `{"status":"ok"}`)
			return err
		}
		if inv.Output.Verbose() {
			_, err := fmt.Fprintln(inv.Env.Stdout, "status: ok (checked 3 sources)")
			return err
		}
		_, err := fmt.Fprintln(inv.Env.Stdout, "status: ok")
		return err
	}

	var out strings.Builder
	for _, o := range []spec.Output{
		{},
		spec.NewOutput(spec.OutputOptions{Verbose: true}),
		spec.NewOutput(spec.OutputOptions{JSON: true}),
	} {
		inv := spec.NewInvocation(&spec.Env{Stdout: &out}, nil, nil)
		inv.Output = o
		if err := report(context.Background(), inv); err != nil {
			fmt.Println(err)
			return
		}
	}
	fmt.Print(out.String())
	fmt.Println(spec.Output{}.Color())
	// Output:
	// status: ok
	// status: ok (checked 3 sources)
	// {"status":"ok"}
	// true
}

func ExampleCommandInfo() {
	selfUpdate := &spec.Command{
		Name:        "self-update",
		Run:         func(context.Context, *spec.Invocation) error { return nil },
		Annotations: map[string]string{spec.SkipUpdateCheck: "true"},
	}

	// An adapter builds the view of the command it runs and passes it to each hook.
	info := spec.NewCommandInfo([]string{"tool", selfUpdate.Name}, selfUpdate.Annotations)
	_, skip := info.Annotation(spec.SkipUpdateCheck)
	fmt.Println(info)
	fmt.Println(info.Path())
	fmt.Println(skip)
	// Output:
	// tool self-update
	// [tool self-update]
	// true
}

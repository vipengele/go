package spec_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/commons/validate"
)

func run(context.Context, *spec.Invocation) error { return nil }

func validTree() *spec.Command {
	return &spec.Command{
		Name:  "tool",
		Flags: []spec.Flag{spec.BoolFlag("verbose", "log more", spec.Shorthand("v"), spec.Inherited())},
		Commands: []*spec.Command{
			{
				Name: "remote",
				Flags: []spec.Flag{
					spec.StringFlag("format", "output format", spec.Shorthand("f"), spec.Inherited()),
				},
				Commands: []*spec.Command{
					{
						Name:  "add",
						Args:  spec.ExactArgs(2),
						Flags: []spec.Flag{spec.IntFlag("timeout", "seconds", spec.Shorthand("t"))},
						Run:   run,
					},
					{Name: "list", Run: run, Hidden: true},
				},
			},
			{Name: "status", Args: spec.AnyArgs(), Run: run},
		},
	}
}

func TestCommandValidateAcceptsValidTree(t *testing.T) {
	if err := validTree().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestCommandValidateAcceptsAllowedReuse(t *testing.T) {
	tests := []struct {
		name string
		cmd  *spec.Command
	}{
		{
			name: "a local flag of the parent may share its name with a flag of a child",
			cmd: &spec.Command{
				Name:  "tool",
				Flags: []spec.Flag{spec.BoolFlag("force", "", spec.Shorthand("f"))},
				Commands: []*spec.Command{
					{Name: "rm", Run: run, Flags: []spec.Flag{spec.BoolFlag("force", "", spec.Shorthand("f"))}},
				},
			},
		},
		{
			name: "commands on different branches may share a name",
			cmd: &spec.Command{
				Name: "tool",
				Commands: []*spec.Command{
					{Name: "a", Commands: []*spec.Command{{Name: "list", Run: run}}},
					{Name: "b", Commands: []*spec.Command{{Name: "list", Run: run}}},
				},
			},
		},
		{
			name: "inherited flags on different branches may share a name",
			cmd: &spec.Command{
				Name: "tool",
				Commands: []*spec.Command{
					{Name: "a", Run: run, Flags: []spec.Flag{spec.BoolFlag("x", "", spec.Inherited())}},
					{Name: "b", Run: run, Flags: []spec.Flag{spec.BoolFlag("x", "", spec.Inherited())}},
				},
			},
		},
		{
			name: "a command may have both Run and Commands",
			cmd:  &spec.Command{Name: "tool", Run: run, Commands: []*spec.Command{{Name: "sub", Run: run}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cmd.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestCommandValidateRejects(t *testing.T) {
	tests := []struct {
		name      string
		cmd       func() *spec.Command
		wantPath  string
		wantInErr string
		wantEmpty bool
	}{
		{
			name:      "nil command",
			cmd:       func() *spec.Command { return nil },
			wantPath:  "",
			wantInErr: "command is nil",
		},
		{
			name:      "empty root name",
			cmd:       func() *spec.Command { return &spec.Command{Run: run} },
			wantPath:  "",
			wantInErr: "Name",
			wantEmpty: true,
		},
		{
			name:      "uppercase name",
			cmd:       func() *spec.Command { return &spec.Command{Name: "Tool", Run: run} },
			wantPath:  "Tool",
			wantInErr: "Name",
		},
		{
			name: "invalid subcommand name",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[0].Commands[0].Name = "add item"
				return c
			},
			wantPath:  "tool remote add item",
			wantInErr: "Name",
		},
		{
			name:      "neither Run nor Commands",
			cmd:       func() *spec.Command { return &spec.Command{Name: "tool"} },
			wantPath:  "tool",
			wantInErr: "neither Run nor Commands",
		},
		{
			name: "leaf without Run",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Run = nil
				return c
			},
			wantPath:  "tool status",
			wantInErr: "neither Run nor Commands",
		},
		{
			name: "nil subcommand",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands = append(c.Commands, nil)
				return c
			},
			wantPath:  "tool",
			wantInErr: "Commands[2] is nil",
		},
		{
			name: "nil flag",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{nil}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "Flags[0] is nil",
		},
		{
			name: "duplicate sibling names",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[0].Commands[1].Name = "add"
				return c
			},
			wantPath:  "tool remote",
			wantInErr: `subcommand "add" is declared twice`,
		},
		{
			name: "negative ExactArgs",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Args = spec.ExactArgs(-1)
				return c
			},
			wantPath:  "tool status",
			wantInErr: "negative",
		},
		{
			name: "negative RangeArgs",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Args = spec.RangeArgs(-2, 1)
				return c
			},
			wantPath:  "tool status",
			wantInErr: "negative",
		},
		{
			name: "RangeArgs minimum above maximum",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Args = spec.RangeArgs(3, 1)
				return c
			},
			wantPath:  "tool status",
			wantInErr: "exceeds its maximum",
		},
		{
			name: "invalid flag name",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{spec.BoolFlag("All", "")}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "flag name",
		},
		{
			name: "empty flag name",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{spec.BoolFlag("", "")}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "flag name",
			wantEmpty: true,
		},
		{
			name: "multi-character shorthand",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{spec.BoolFlag("all", "", spec.Shorthand("al"))}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "flag shorthand",
		},
		{
			name: "punctuation shorthand",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{spec.BoolFlag("all", "", spec.Shorthand("-"))}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "flag shorthand",
		},
		{
			name: "default of another type",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{spec.IntFlag("count", "", spec.Default("three"))}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "default of type string for a int flag",
		},
		{
			name: "flag name repeated in one command",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{spec.BoolFlag("all", ""), spec.StringFlag("all", "")}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "flag --all is declared twice",
		},
		{
			name: "one handle listed twice",
			cmd: func() *spec.Command {
				c := validTree()
				all := spec.BoolFlag("all", "")
				c.Commands[1].Flags = []spec.Flag{all, all}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "flag --all is declared twice",
		},
		{
			name: "shorthand repeated in one command",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{
					spec.BoolFlag("all", "", spec.Shorthand("a")),
					spec.BoolFlag("any", "", spec.Shorthand("a")),
				}
				return c
			},
			wantPath:  "tool status",
			wantInErr: "shorthand -a is declared twice",
		},
		{
			name: "local flag collides with an inherited flag of the parent",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[1].Flags = []spec.Flag{spec.BoolFlag("verbose", "")}
				return c
			},
			wantPath:  "tool status",
			wantInErr: `flag --verbose collides with the flag inherited from command "tool"`,
		},
		{
			name: "local flag collides with an inherited flag of a grandparent",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[0].Commands[0].Flags = []spec.Flag{spec.BoolFlag("verbose", "")}
				return c
			},
			wantPath:  "tool remote add",
			wantInErr: `flag --verbose collides with the flag inherited from command "tool"`,
		},
		{
			name: "inherited flag collides with an inherited flag of an ancestor",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[0].Flags = []spec.Flag{spec.BoolFlag("verbose", "", spec.Inherited())}
				return c
			},
			wantPath:  "tool remote",
			wantInErr: `flag --verbose collides with the flag inherited from command "tool"`,
		},
		{
			name: "shorthand collides with an inherited shorthand",
			cmd: func() *spec.Command {
				c := validTree()
				c.Commands[0].Commands[0].Flags = []spec.Flag{spec.BoolFlag("force", "", spec.Shorthand("f"))}
				return c
			},
			wantPath:  "tool remote add",
			wantInErr: `shorthand -f of flag --force collides with the flag inherited from command "tool remote"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cmd().Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("Validate() = %q, want it to contain %q", err, tc.wantInErr)
			}
			var ce *spec.CommandError
			if !errors.As(err, &ce) {
				t.Fatalf("Validate() error %T holds no *spec.CommandError", err)
			}
			if ce.Path != tc.wantPath {
				t.Errorf("CommandError.Path = %q, want %q", ce.Path, tc.wantPath)
			}
			if tc.wantEmpty {
				if !errors.Is(err, validate.ErrEmpty) {
					t.Errorf("Validate() = %v, want ErrEmpty", err)
				}
			} else if !errors.Is(err, validate.ErrInvalid) {
				t.Errorf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCommandValidateReportsEveryProblem(t *testing.T) {
	c := validTree()
	c.Name = "Tool"
	c.Commands[1].Run = nil
	c.Commands[0].Commands[0].Args = spec.RangeArgs(2, 1)
	c.Commands[0].Commands[1].Flags = []spec.Flag{spec.BoolFlag("verbose", "")}

	err := c.Validate()
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("error %T does not join its causes", err)
	}
	want := map[string]bool{
		"Tool":             true,
		"Tool status":      true,
		"Tool remote add":  true,
		"Tool remote list": true,
	}
	causes := joined.Unwrap()
	if len(causes) != len(want) {
		t.Errorf("Validate() reported %d problems, want %d: %v", len(causes), len(want), err)
	}
	for _, cause := range causes {
		var ce *spec.CommandError
		if !errors.As(cause, &ce) {
			t.Fatalf("cause %T is not a *spec.CommandError", cause)
		}
		if !want[ce.Path] {
			t.Errorf("unexpected problem at %q: %v", ce.Path, ce)
		}
		delete(want, ce.Path)
	}
	if len(want) != 0 {
		t.Errorf("no problem reported for %v", want)
	}
}

func TestCommandValidateDoesNotModifyTheTree(t *testing.T) {
	c := validTree()
	c.Commands[1].Run = nil
	before := *c.Commands[1]
	_ = c.Validate()
	if c.Commands[1].Name != before.Name || c.Commands[1].Run != nil || len(c.Commands) != 2 {
		t.Fatal("Validate modified the tree")
	}
}

func TestCommandErrorMessage(t *testing.T) {
	err := &spec.CommandError{Path: "tool sub", Err: errors.New("boom")}
	if got, want := err.Error(), `command "tool sub": boom`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestCommandValidateAcceptsAnnotations(t *testing.T) {
	tree := validTree()
	tree.Annotations = map[string]string{spec.SkipUpdateCheck: ""}
	tree.Commands[1].Annotations = map[string]string{"any.key": "any value"}
	if err := tree.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

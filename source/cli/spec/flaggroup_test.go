package spec_test

import (
	"errors"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/commons/validate"
)

// groupTree returns a root declaring the inherited --verbose, as cli/core declares a standard
// flag, and the local --force, with a subcommand "remote" declaring the inherited --format and
// the local --json, and a leaf "add" below it declaring --timeout and carrying groups.
func groupTree(groups ...spec.FlagGroup) *spec.Command {
	return &spec.Command{
		Name: "tool",
		Flags: []spec.Flag{
			spec.BoolFlag("verbose", "", spec.Shorthand("v"), spec.Inherited()),
			spec.BoolFlag("force", ""),
		},
		Commands: []*spec.Command{{
			Name: "remote",
			Flags: []spec.Flag{
				spec.StringFlag("format", "", spec.Inherited()),
				spec.BoolFlag("json", ""),
			},
			Commands: []*spec.Command{{
				Name:       "add",
				Run:        run,
				Flags:      []spec.Flag{spec.IntFlag("timeout", ""), spec.BoolFlag("wait", "")},
				FlagGroups: groups,
			}},
		}},
	}
}

func TestFlagGroupKindString(t *testing.T) {
	tests := []struct {
		kind spec.FlagGroupKind
		want string
	}{
		{spec.MutuallyExclusive, "mutually exclusive"},
		{spec.RequiredTogether, "required together"},
		{0, "FlagGroupKind(0)"},
		{9, "FlagGroupKind(9)"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("FlagGroupKind(%d).String() = %q, want %q", int(tt.kind), got, tt.want)
		}
	}
}

func TestCommandValidateAcceptsFlagGroups(t *testing.T) {
	tests := []struct {
		name  string
		group spec.FlagGroup
	}{
		{"mutually exclusive own flags", spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"timeout", "wait"}}},
		{"required together own flags", spec.FlagGroup{Kind: spec.RequiredTogether, Flags: []string{"timeout", "wait"}}},
		{"a flag inherited from the parent", spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"format", "wait"}}},
		{"a standard flag inherited from the root", spec.FlagGroup{Kind: spec.RequiredTogether, Flags: []string{"verbose", "timeout", "format"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := groupTree(tt.group).Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestCommandValidateAcceptsFlagGroupOnRootWithRun(t *testing.T) {
	root := groupTree()
	root.Run = run
	root.FlagGroups = []spec.FlagGroup{{Kind: spec.MutuallyExclusive, Flags: []string{"verbose", "force"}}}
	if err := root.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestCommandValidateRejectsFlagGroupsWithoutRun(t *testing.T) {
	exclusive := spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"verbose", "json"}}
	together := spec.FlagGroup{Kind: spec.RequiredTogether, Flags: []string{"format", "json"}}
	tests := []struct {
		name string
		set  func(root *spec.Command)
		path string
	}{
		{"the root", func(root *spec.Command) {
			root.FlagGroups = []spec.FlagGroup{{Kind: spec.MutuallyExclusive, Flags: []string{"verbose", "force"}}}
		}, "tool"},
		{"a nested command", func(root *spec.Command) { root.Commands[0].FlagGroups = []spec.FlagGroup{exclusive} }, "tool remote"},
		{"several groups", func(root *spec.Command) { root.Commands[0].FlagGroups = []spec.FlagGroup{exclusive, together} }, "tool remote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := groupTree()
			tt.set(root)
			err := root.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			var joined interface{ Unwrap() []error }
			if !errors.As(err, &joined) || len(joined.Unwrap()) != 1 {
				t.Fatalf("Validate() = %v, want exactly one problem", err)
			}
			var ce *spec.CommandError
			if !errors.As(err, &ce) {
				t.Fatalf("Validate() = %v, want a *CommandError", err)
			}
			if ce.Path != tt.path {
				t.Errorf("Path = %q, want %q", ce.Path, tt.path)
			}
			if !errors.Is(err, validate.ErrInvalid) {
				t.Errorf("Validate() = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), "FlagGroups on a command with no Run") {
				t.Errorf("Validate() = %q, want it to name FlagGroups on a command with no Run", err)
			}
		})
	}
}

func TestCommandValidateRejectsFlagGroupsWithoutRunAndTheirOwnProblems(t *testing.T) {
	root := groupTree()
	root.FlagGroups = []spec.FlagGroup{{Kind: spec.MutuallyExclusive, Flags: []string{"verbose"}}}
	err := root.Validate()
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) || len(joined.Unwrap()) != 2 {
		t.Fatalf("Validate() = %v, want exactly two problems", err)
	}
	for _, want := range []string{"FlagGroups on a command with no Run", "FlagGroups[0] names 1 flags"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %q, want it to contain %q", err, want)
		}
	}
}

func TestCommandValidateRejectsFlagGroups(t *testing.T) {
	tests := []struct {
		name  string
		group spec.FlagGroup
		want  string
	}{
		{"no flags", spec.FlagGroup{Kind: spec.MutuallyExclusive}, "names 0 flags"},
		{"one flag", spec.FlagGroup{Kind: spec.RequiredTogether, Flags: []string{"wait"}}, "names 1 flags"},
		{"an unknown flag", spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"wait", "missing"}}, "--missing"},
		{
			"a local flag of the parent",
			spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"wait", "json"}},
			"--json, which the command neither declares nor inherits",
		},
		{
			"a local flag of the root",
			spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"wait", "force"}},
			"--force, which the command neither declares nor inherits",
		},
		{"a flag named with dashes", spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"wait", "--timeout"}}, "--timeout, which the command neither declares nor inherits"},
		{"a repeated flag", spec.FlagGroup{Kind: spec.RequiredTogether, Flags: []string{"wait", "timeout", "wait"}}, "names flag --wait twice"},
		{"the zero kind", spec.FlagGroup{Flags: []string{"wait", "timeout"}}, "unknown kind FlagGroupKind(0)"},
		{"an unknown kind", spec.FlagGroup{Kind: 7, Flags: []string{"wait", "timeout"}}, "unknown kind FlagGroupKind(7)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := groupTree(spec.FlagGroup{Kind: spec.MutuallyExclusive, Flags: []string{"timeout", "wait"}}, tt.group).Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			var joined interface{ Unwrap() []error }
			if !errors.As(err, &joined) || len(joined.Unwrap()) != 1 {
				t.Fatalf("Validate() = %v, want exactly one problem", err)
			}
			var ce *spec.CommandError
			if !errors.As(err, &ce) {
				t.Fatalf("Validate() = %v, want a *CommandError", err)
			}
			if ce.Path != "tool remote add" {
				t.Errorf("Path = %q, want %q", ce.Path, "tool remote add")
			}
			if !errors.Is(err, validate.ErrInvalid) {
				t.Errorf("Validate() = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), "FlagGroups[1]") || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() = %q, want it to name FlagGroups[1] and contain %q", err, tt.want)
			}
		})
	}
}

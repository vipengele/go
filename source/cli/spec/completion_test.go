package spec_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestCompletionAccessors(t *testing.T) {
	fn := func(context.Context, spec.CompletionInput) ([]string, error) { return []string{"x"}, nil }
	tests := []struct {
		name           string
		c              spec.Completion
		wantKind       spec.CompletionKind
		wantChoices    []string
		wantExtensions []string
		wantFunc       bool
	}{
		{name: "zero value", c: spec.Completion{}, wantKind: spec.NoCompletion},
		{
			name: "choices", c: spec.CompleteChoices("text", "json"),
			wantKind: spec.ChoicesCompletion, wantChoices: []string{"text", "json"},
		},
		{name: "choices without values", c: spec.CompleteChoices(), wantKind: spec.ChoicesCompletion},
		{name: "any file", c: spec.CompleteFiles(), wantKind: spec.FilesCompletion},
		{
			name: "files by extension, with or without the dot", c: spec.CompleteFiles("yaml", ".yml"),
			wantKind: spec.FilesCompletion, wantExtensions: []string{"yaml", "yml"},
		},
		{name: "dirs", c: spec.CompleteDirs(), wantKind: spec.DirsCompletion},
		{name: "func", c: spec.CompleteFunc(fn), wantKind: spec.FuncCompletion, wantFunc: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Kind(); got != tc.wantKind {
				t.Errorf("Kind() = %v, want %v", got, tc.wantKind)
			}
			if got := tc.c.Choices(); !slices.Equal(got, tc.wantChoices) {
				t.Errorf("Choices() = %q, want %q", got, tc.wantChoices)
			}
			if got := tc.c.Extensions(); !slices.Equal(got, tc.wantExtensions) {
				t.Errorf("Extensions() = %q, want %q", got, tc.wantExtensions)
			}
			if got := tc.c.Func() != nil; got != tc.wantFunc {
				t.Errorf("Func() != nil is %v, want %v", got, tc.wantFunc)
			}
		})
	}
}

func TestCompleteFuncReturnsTheGivenFunction(t *testing.T) {
	var got spec.CompletionInput
	c := spec.CompleteFunc(func(_ context.Context, in spec.CompletionInput) ([]string, error) {
		got = in
		return []string{"alpha", "beta"}, nil
	})
	in := spec.CompletionInput{ToComplete: "al", Args: []string{"first"}}
	values, err := c.Func()(context.Background(), in)
	if err != nil {
		t.Fatalf("Func()() error = %v, want nil", err)
	}
	if !slices.Equal(values, []string{"alpha", "beta"}) {
		t.Errorf("Func()() = %q, want [alpha beta]", values)
	}
	if got.ToComplete != "al" || !slices.Equal(got.Args, []string{"first"}) {
		t.Errorf("Func() received %+v, want %+v", got, in)
	}
}

func TestCompleteFuncPassesTheErrorThrough(t *testing.T) {
	boom := errors.New("boom")
	c := spec.CompleteFunc(func(context.Context, spec.CompletionInput) ([]string, error) { return nil, boom })
	if _, err := c.Func()(context.Background(), spec.CompletionInput{}); !errors.Is(err, boom) {
		t.Errorf("Func()() error = %v, want %v", err, boom)
	}
}

func TestCompletionIsImmutable(t *testing.T) {
	values := []string{"a", "b"}
	c := spec.CompleteChoices(values...)
	values[0] = "changed"
	c.Choices()[1] = "changed"
	if got := c.Choices(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Choices() = %q, want [a b]", got)
	}

	exts := []string{"yaml"}
	f := spec.CompleteFiles(exts...)
	exts[0] = "changed"
	f.Extensions()[0] = "changed"
	if got := f.Extensions(); !slices.Equal(got, []string{"yaml"}) {
		t.Errorf("Extensions() = %q, want [yaml]", got)
	}
}

func TestCompletionKindString(t *testing.T) {
	tests := map[spec.CompletionKind]string{
		spec.NoCompletion:       "none",
		spec.ChoicesCompletion:  "choices",
		spec.FilesCompletion:    "files",
		spec.DirsCompletion:     "dirs",
		spec.FuncCompletion:     "func",
		spec.CompletionKind(99): "CompletionKind(99)",
	}
	for k, want := range tests {
		if got := k.String(); got != want {
			t.Errorf("CompletionKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}

func TestFlagCompletion(t *testing.T) {
	tests := []struct {
		name string
		flag spec.Flag
		want spec.CompletionKind
	}{
		{name: "string flag without Complete", flag: spec.StringFlag("output", ""), want: spec.NoCompletion},
		{
			name: "string flag with choices",
			flag: spec.StringFlag("output", "", spec.Complete(spec.CompleteChoices("text", "json"))),
			want: spec.ChoicesCompletion,
		},
		{
			name: "int flag with a function",
			flag: spec.IntFlag("port", "", spec.Complete(spec.CompleteFunc(
				func(context.Context, spec.CompletionInput) ([]string, error) { return nil, nil }))),
			want: spec.FuncCompletion,
		},
		{name: "bool flag without Complete", flag: spec.BoolFlag("json", ""), want: spec.NoCompletion},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.flag.Completion().Kind(); got != tc.want {
				t.Errorf("Completion().Kind() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFlagCompleteKeepsTheValues(t *testing.T) {
	f := spec.StringFlag("config", "", spec.Complete(spec.CompleteFiles("yaml")))
	if got := f.Completion().Extensions(); !slices.Equal(got, []string{"yaml"}) {
		t.Errorf("Completion().Extensions() = %q, want [yaml]", got)
	}
}

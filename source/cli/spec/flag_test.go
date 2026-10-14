package spec_test

import (
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestFlagMetadata(t *testing.T) {
	tests := []struct {
		name          string
		flag          spec.Flag
		wantName      string
		wantShorthand string
		wantUsage     string
		wantKind      spec.FlagKind
		wantDefault   any
		wantInherited bool
	}{
		{
			name:     "bool without options",
			flag:     spec.BoolFlag("json", "print JSON"),
			wantName: "json", wantUsage: "print JSON", wantKind: spec.BoolKind, wantDefault: false,
		},
		{
			name:     "bool with every option",
			flag:     spec.BoolFlag("verbose", "log more", spec.Shorthand("v"), spec.Inherited(), spec.Default(true)),
			wantName: "verbose", wantShorthand: "v", wantUsage: "log more", wantKind: spec.BoolKind,
			wantDefault: true, wantInherited: true,
		},
		{
			name:     "string with default",
			flag:     spec.StringFlag("output", "output format", spec.Shorthand("o"), spec.Default("text")),
			wantName: "output", wantShorthand: "o", wantUsage: "output format", wantKind: spec.StringKind,
			wantDefault: "text",
		},
		{
			name:     "int with default",
			flag:     spec.IntFlag("retries", "attempts", spec.Default(3), spec.Inherited()),
			wantName: "retries", wantUsage: "attempts", wantKind: spec.IntKind, wantDefault: 3,
			wantInherited: true,
		},
		{
			name:     "nil option is ignored",
			flag:     spec.IntFlag("count", "how many", nil),
			wantName: "count", wantUsage: "how many", wantKind: spec.IntKind, wantDefault: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.flag
			if got := f.Name(); got != tc.wantName {
				t.Errorf("Name() = %q, want %q", got, tc.wantName)
			}
			if got := f.Shorthand(); got != tc.wantShorthand {
				t.Errorf("Shorthand() = %q, want %q", got, tc.wantShorthand)
			}
			if got := f.Usage(); got != tc.wantUsage {
				t.Errorf("Usage() = %q, want %q", got, tc.wantUsage)
			}
			if got := f.Kind(); got != tc.wantKind {
				t.Errorf("Kind() = %v, want %v", got, tc.wantKind)
			}
			if got := f.DefaultValue(); got != tc.wantDefault {
				t.Errorf("DefaultValue() = %#v, want %#v", got, tc.wantDefault)
			}
			if got := f.Inherited(); got != tc.wantInherited {
				t.Errorf("Inherited() = %t, want %t", got, tc.wantInherited)
			}
		})
	}
}

func TestFlagKindString(t *testing.T) {
	tests := []struct {
		kind spec.FlagKind
		want string
	}{
		{spec.BoolKind, "bool"},
		{spec.StringKind, "string"},
		{spec.IntKind, "int"},
		{spec.FlagKind(0), "FlagKind(0)"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("FlagKind(%d).String() = %q, want %q", int(tc.kind), got, tc.want)
		}
	}
}

func TestFlagGetReadsTheInvocation(t *testing.T) {
	asJSON := spec.BoolFlag("json", "print JSON")
	output := spec.StringFlag("output", "format", spec.Default("text"))
	retries := spec.IntFlag("retries", "attempts", spec.Default(3))

	inv := spec.NewInvocation(nil, nil, map[string]any{
		"json":    true,
		"output":  "yaml",
		"retries": 7,
	})
	if got := asJSON.Get(inv); !got {
		t.Errorf("json Get = %t, want true", got)
	}
	if got := output.Get(inv); got != "yaml" {
		t.Errorf("output Get = %q, want %q", got, "yaml")
	}
	if got := retries.Get(inv); got != 7 {
		t.Errorf("retries Get = %d, want 7", got)
	}
}

func TestFlagGetFallsBackToTheDefault(t *testing.T) {
	output := spec.StringFlag("output", "format", spec.Default("text"))
	retries := spec.IntFlag("retries", "attempts", spec.Default(3))
	verbose := spec.BoolFlag("verbose", "log more", spec.Default(true))

	tests := []struct {
		name string
		inv  *spec.Invocation
	}{
		{"nil invocation", nil},
		{"zero invocation", &spec.Invocation{}},
		{"nil values", spec.NewInvocation(nil, nil, nil)},
		{"missing keys", spec.NewInvocation(nil, nil, map[string]any{"other": "x"})},
		{"mismatched types", spec.NewInvocation(nil, nil, map[string]any{
			"output": 5, "retries": "7", "verbose": "false",
		})},
		{"nil values in map", spec.NewInvocation(nil, nil, map[string]any{
			"output": nil, "retries": nil, "verbose": nil,
		})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := output.Get(tc.inv); got != "text" {
				t.Errorf("output Get = %q, want %q", got, "text")
			}
			if got := retries.Get(tc.inv); got != 3 {
				t.Errorf("retries Get = %d, want 3", got)
			}
			if got := verbose.Get(tc.inv); !got {
				t.Errorf("verbose Get = %t, want true", got)
			}
		})
	}
}

func TestFlagGetWithoutDefaultIsZero(t *testing.T) {
	inv := spec.NewInvocation(nil, nil, nil)
	if got := spec.BoolFlag("b", "").Get(inv); got {
		t.Errorf("bool Get = %t, want false", got)
	}
	if got := spec.StringFlag("s", "").Get(inv); got != "" {
		t.Errorf("string Get = %q, want empty", got)
	}
	if got := spec.IntFlag("i", "").Get(inv); got != 0 {
		t.Errorf("int Get = %d, want 0", got)
	}
}

func TestFlagMistypedDefaultIsIgnored(t *testing.T) {
	f := spec.StringFlag("output", "format", spec.Default(5))
	if got := f.Default(); got != "" {
		t.Errorf("Default() = %q, want empty", got)
	}
	if got := f.Get(nil); got != "" {
		t.Errorf("Get(nil) = %q, want empty", got)
	}
}

func TestFlagGetSetValueEqualToZeroOverridesDefault(t *testing.T) {
	verbose := spec.BoolFlag("verbose", "log more", spec.Default(true))
	inv := spec.NewInvocation(nil, nil, map[string]any{"verbose": false})
	if got := verbose.Get(inv); got {
		t.Errorf("Get = %t, want false", got)
	}
}

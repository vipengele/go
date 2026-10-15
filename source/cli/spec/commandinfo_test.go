package spec_test

import (
	"maps"
	"slices"
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestCommandInfoReadsWhatItWasGiven(t *testing.T) {
	tests := []struct {
		name        string
		info        spec.CommandInfo
		path        []string
		str         string
		annotations map[string]string
	}{
		{"zero value", spec.CommandInfo{}, nil, "", map[string]string{}},
		{"nil inputs", spec.NewCommandInfo(nil, nil), nil, "", map[string]string{}},
		{"root", spec.NewCommandInfo([]string{"tool"}, nil), []string{"tool"}, "tool", map[string]string{}},
		{
			"nested with annotations",
			spec.NewCommandInfo([]string{"tool", "remote", "add"}, map[string]string{spec.SkipUpdateCheck: ""}),
			[]string{"tool", "remote", "add"},
			"tool remote add",
			map[string]string{spec.SkipUpdateCheck: ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.Path(); !slices.Equal(got, tt.path) {
				t.Errorf("Path() = %q, want %q", got, tt.path)
			}
			if got := tt.info.String(); got != tt.str {
				t.Errorf("String() = %q, want %q", got, tt.str)
			}
			got := tt.info.Annotations()
			if got == nil {
				t.Error("Annotations() is nil")
			}
			if !maps.Equal(got, tt.annotations) {
				t.Errorf("Annotations() = %v, want %v", got, tt.annotations)
			}
			for k, want := range tt.annotations {
				if v, ok := tt.info.Annotation(k); !ok || v != want {
					t.Errorf("Annotation(%q) = %q, %t, want %q, true", k, v, ok, want)
				}
			}
			if v, ok := tt.info.Annotation("absent"); ok || v != "" {
				t.Errorf("Annotation(absent) = %q, %t, want \"\", false", v, ok)
			}
		})
	}
}

func TestCommandInfoIsReadOnly(t *testing.T) {
	path := []string{"tool", "status"}
	annotations := map[string]string{"k": "v"}
	info := spec.NewCommandInfo(path, annotations)

	path[0] = "changed"
	annotations["k"] = "changed"
	info.Path()[1] = "changed"
	info.Annotations()["k"] = "changed"

	if got := info.Path(); !slices.Equal(got, []string{"tool", "status"}) {
		t.Errorf("Path() = %q after callers changed their copies, want [tool status]", got)
	}
	if v, _ := info.Annotation("k"); v != "v" {
		t.Errorf("Annotation(k) = %q after callers changed their copies, want v", v)
	}
}

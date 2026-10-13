package envvar_test

import (
	"errors"
	"strings"
	"testing"

	"go.vipengele.org/commons/envvar"
	"go.vipengele.org/commons/validate"
)

func TestName(t *testing.T) {
	cases := []struct {
		name         string
		prefix       string
		suffix       string
		wantName     string
		wantEmpty    bool
		wantInvalid  bool
		wantMentions []string
		wantAbsent   []string
	}{
		{name: "joins prefix and suffix with an underscore", prefix: "MYAPP", suffix: "NO_COLOR", wantName: "MYAPP_NO_COLOR"},
		{name: "single letter parts", prefix: "A", suffix: "B", wantName: "A_B"},
		{name: "digits in both parts", prefix: "APP2", suffix: "V3", wantName: "APP2_V3"},
		{
			name: "joined name at the bound is accepted", prefix: strings.Repeat("A", 31),
			suffix: strings.Repeat("B", validate.MaxEnvVarLen-32), wantName: strings.Repeat("A", 31) + "_" + strings.Repeat("B", validate.MaxEnvVarLen-32),
		},
		{
			name: "hyphenated suffix is not converted", prefix: "MYAPP", suffix: "no-color",
			wantInvalid: true, wantMentions: []string{"suffix", `"no-color"`}, wantAbsent: []string{"prefix"},
		},
		{
			name: "lowercase suffix is not upper-cased", prefix: "MYAPP", suffix: "color",
			wantInvalid: true, wantMentions: []string{"suffix"}, wantAbsent: []string{"prefix"},
		},
		{
			name: "lowercase prefix is not upper-cased", prefix: "myapp", suffix: "COLOR",
			wantInvalid: true, wantMentions: []string{"prefix", `"myapp"`}, wantAbsent: []string{"suffix"},
		},
		{
			name: "empty prefix", prefix: "", suffix: "COLOR",
			wantEmpty: true, wantMentions: []string{"prefix"}, wantAbsent: []string{"suffix"},
		},
		{
			name: "empty suffix", prefix: "MYAPP", suffix: "",
			wantEmpty: true, wantMentions: []string{"suffix"}, wantAbsent: []string{"prefix"},
		},
		{
			name: "trailing underscore in the prefix", prefix: "MYAPP_", suffix: "COLOR",
			wantInvalid: true, wantMentions: []string{"prefix"}, wantAbsent: []string{"suffix"},
		},
		{
			name: "trailing underscore in the suffix", prefix: "MYAPP", suffix: "COLOR_",
			wantInvalid: true, wantMentions: []string{"suffix"}, wantAbsent: []string{"prefix"},
		},
		{
			name: "leading underscore in the suffix", prefix: "MYAPP", suffix: "_COLOR",
			wantInvalid: true, wantMentions: []string{"suffix"}, wantAbsent: []string{"prefix"},
		},
		{
			name: "both parts invalid are both reported", prefix: "my-app", suffix: "no-color",
			wantInvalid: true, wantMentions: []string{"prefix", "suffix"},
		},
		{
			name: "both parts empty are both reported", prefix: "", suffix: "",
			wantEmpty: true, wantMentions: []string{"prefix", "suffix"},
		},
		{
			name: "empty and invalid parts report both sentinels", prefix: "", suffix: "bad",
			wantEmpty: true, wantInvalid: true, wantMentions: []string{"prefix", "suffix"},
		},
		{
			name: "joined name over the bound fails as name", prefix: strings.Repeat("A", 32),
			suffix:      strings.Repeat("B", validate.MaxEnvVarLen-31),
			wantInvalid: true, wantMentions: []string{"name"}, wantAbsent: []string{"prefix", "suffix"},
		},
		{
			name: "two parts at the bound join over it", prefix: strings.Repeat("A", validate.MaxEnvVarLen),
			suffix:      strings.Repeat("B", validate.MaxEnvVarLen),
			wantInvalid: true, wantMentions: []string{"name"}, wantAbsent: []string{"prefix", "suffix"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := envvar.Name(tc.prefix, tc.suffix)
			if !tc.wantEmpty && !tc.wantInvalid {
				if err != nil {
					t.Fatalf("Name(%q, %q) error = %v, want nil", tc.prefix, tc.suffix, err)
				}
				if got != tc.wantName {
					t.Fatalf("Name(%q, %q) = %q, want %q", tc.prefix, tc.suffix, got, tc.wantName)
				}
				return
			}
			if err == nil {
				t.Fatalf("Name(%q, %q) = %q, want an error", tc.prefix, tc.suffix, got)
			}
			if got != "" {
				t.Errorf("name = %q on failure, want empty", got)
			}
			if got := errors.Is(err, validate.ErrEmpty); got != tc.wantEmpty {
				t.Errorf("errors.Is(err, ErrEmpty) = %v, want %v", got, tc.wantEmpty)
			}
			if got := errors.Is(err, validate.ErrInvalid); got != tc.wantInvalid {
				t.Errorf("errors.Is(err, ErrInvalid) = %v, want %v", got, tc.wantInvalid)
			}
			var fe *validate.FieldError
			if !errors.As(err, &fe) {
				t.Errorf("error %T carries no *FieldError", err)
			}
			for _, want := range tc.wantMentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error text %q does not mention %q", err.Error(), want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(err.Error(), absent) {
					t.Errorf("error text %q mentions %q", err.Error(), absent)
				}
			}
		})
	}
}

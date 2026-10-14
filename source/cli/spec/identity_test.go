package spec_test

import (
	"errors"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/commons/validate"
)

func validIdentity() spec.Identity {
	return spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
}

func TestIdentityValidateAcceptsValidIdentity(t *testing.T) {
	if err := validIdentity().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestIdentityValidateFields(t *testing.T) {
	longPrefix := strings.Repeat("A", spec.MaxEnvPrefixLen)

	tests := []struct {
		name      string
		set       func(*spec.Identity)
		field     string
		value     string
		wantEmpty bool
		wantErr   bool
	}{
		{"empty Name", func(i *spec.Identity) { i.Name = "" }, "Name", "", true, true},
		{"uppercase in Name", func(i *spec.Identity) { i.Name = "Atk" }, "Name", "Atk", false, true},
		{"slash in Name", func(i *spec.Identity) { i.Name = "a/tk" }, "Name", "a/tk", false, true},
		{"whitespace in Name", func(i *spec.Identity) { i.Name = "a tk" }, "Name", "a tk", false, true},
		{"non-ASCII in Name", func(i *spec.Identity) { i.Name = "átk" }, "Name", "átk", false, true},
		{"surrounding space in Name is not trimmed", func(i *spec.Identity) { i.Name = " atk" }, "Name", " atk", false, true},
		{"empty Owner", func(i *spec.Identity) { i.Owner = "" }, "Owner", "", true, true},
		{"slash in Owner", func(i *spec.Identity) { i.Owner = "a/b" }, "Owner", "a/b", false, true},
		{"whitespace in Owner", func(i *spec.Identity) { i.Owner = "a b" }, "Owner", "a b", false, true},
		{"non-ASCII in Owner", func(i *spec.Identity) { i.Owner = "pédro" }, "Owner", "pédro", false, true},
		{"surrounding space in Owner is not trimmed", func(i *spec.Identity) { i.Owner = "pedro " }, "Owner", "pedro ", false, true},
		{"empty Repo", func(i *spec.Identity) { i.Repo = "" }, "Repo", "", true, true},
		{"slash in Repo", func(i *spec.Identity) { i.Repo = "a/b" }, "Repo", "a/b", false, true},
		{"dot Repo", func(i *spec.Identity) { i.Repo = "." }, "Repo", ".", false, true},
		{"dot-dot Repo", func(i *spec.Identity) { i.Repo = ".." }, "Repo", "..", false, true},
		{"Repo ending in .git", func(i *spec.Identity) { i.Repo = "toolkit.git" }, "Repo", "toolkit.git", false, true},
		{"whitespace in Repo", func(i *spec.Identity) { i.Repo = "tool kit" }, "Repo", "tool kit", false, true},
		{"non-ASCII in Repo", func(i *spec.Identity) { i.Repo = "tóol" }, "Repo", "tóol", false, true},
		{"empty EnvPrefix", func(i *spec.Identity) { i.EnvPrefix = "" }, "EnvPrefix", "", true, true},
		{"lowercase EnvPrefix", func(i *spec.Identity) { i.EnvPrefix = "atk" }, "EnvPrefix", "atk", false, true},
		{"slash in EnvPrefix", func(i *spec.Identity) { i.EnvPrefix = "A/TK" }, "EnvPrefix", "A/TK", false, true},
		{"doubled underscore in EnvPrefix", func(i *spec.Identity) { i.EnvPrefix = "A__TK" }, "EnvPrefix", "A__TK", false, true},
		{"whitespace in EnvPrefix", func(i *spec.Identity) { i.EnvPrefix = "A TK" }, "EnvPrefix", "A TK", false, true},
		{"non-ASCII in EnvPrefix", func(i *spec.Identity) { i.EnvPrefix = "ÁTK" }, "EnvPrefix", "ÁTK", false, true},
		{"surrounding space in EnvPrefix is not trimmed", func(i *spec.Identity) { i.EnvPrefix = " ATK" }, "EnvPrefix", " ATK", false, true},
		{"EnvPrefix at the maximum length", func(i *spec.Identity) { i.EnvPrefix = longPrefix }, "", "", false, false},
		{"EnvPrefix one byte over the maximum length", func(i *spec.Identity) { i.EnvPrefix = longPrefix + "A" }, "EnvPrefix", longPrefix + "A", false, true},
		{"empty DirName", func(i *spec.Identity) { i.DirName = "" }, "DirName", "", true, true},
		{"slash in DirName", func(i *spec.Identity) { i.DirName = "a/b" }, "DirName", "a/b", false, true},
		{"dot DirName", func(i *spec.Identity) { i.DirName = "." }, "DirName", ".", false, true},
		{"dot-dot DirName", func(i *spec.Identity) { i.DirName = ".." }, "DirName", "..", false, true},
		{"leading dot in DirName", func(i *spec.Identity) { i.DirName = ".hidden" }, "DirName", ".hidden", false, true},
		{"uppercase in DirName", func(i *spec.Identity) { i.DirName = "Toolkit" }, "DirName", "Toolkit", false, true},
		{"whitespace in DirName", func(i *spec.Identity) { i.DirName = "tool kit" }, "DirName", "tool kit", false, true},
		{"non-ASCII in DirName", func(i *spec.Identity) { i.DirName = "tóol" }, "DirName", "tóol", false, true},
		{"surrounding space in DirName is not trimmed", func(i *spec.Identity) { i.DirName = "toolkit " }, "DirName", "toolkit ", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := validIdentity()
			tc.set(&id)
			before := id
			err := id.Validate()
			if id != before {
				t.Fatalf("Validate modified the identity: %+v, want %+v", id, before)
			}
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if tc.wantEmpty {
				if !errors.Is(err, validate.ErrEmpty) || errors.Is(err, validate.ErrInvalid) {
					t.Fatalf("Validate() = %v, want ErrEmpty and not ErrInvalid", err)
				}
			} else if !errors.Is(err, validate.ErrInvalid) || errors.Is(err, validate.ErrEmpty) {
				t.Fatalf("Validate() = %v, want ErrInvalid and not ErrEmpty", err)
			}
			var fe *validate.FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("Validate() error %T holds no *validate.FieldError", err)
			}
			if fe.Field != tc.field {
				t.Errorf("FieldError.Field = %q, want %q", fe.Field, tc.field)
			}
			if fe.Value != tc.value {
				t.Errorf("FieldError.Value = %q, want %q", fe.Value, tc.value)
			}
		})
	}
}

func TestIdentityValidateReportsEveryFailingField(t *testing.T) {
	tests := []struct {
		name       string
		id         spec.Identity
		wantFields []string
		wantEmpty  bool
		wantBad    bool
	}{
		{
			name:       "all fields invalid",
			id:         spec.Identity{Name: "Bad", Owner: "a/b", Repo: "..", EnvPrefix: "a__b", DirName: ".hidden"},
			wantFields: []string{"Name", "Owner", "Repo", "EnvPrefix", "DirName"},
			wantBad:    true,
		},
		{
			name:       "zero value",
			id:         spec.Identity{},
			wantFields: []string{"Name", "Owner", "Repo", "EnvPrefix", "DirName"},
			wantEmpty:  true,
		},
		{
			name:       "empty and invalid fields together",
			id:         spec.Identity{Name: "", Owner: "a/b", Repo: "repo", EnvPrefix: "ATK", DirName: "dir"},
			wantFields: []string{"Name", "Owner"},
			wantEmpty:  true,
			wantBad:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.id.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			for _, field := range tc.wantFields {
				if !strings.Contains(err.Error(), field) {
					t.Errorf("error %q does not mention %q", err, field)
				}
			}
			if got := errors.Is(err, validate.ErrEmpty); got != tc.wantEmpty {
				t.Errorf("errors.Is(err, ErrEmpty) = %t, want %t", got, tc.wantEmpty)
			}
			if got := errors.Is(err, validate.ErrInvalid); got != tc.wantBad {
				t.Errorf("errors.Is(err, ErrInvalid) = %t, want %t", got, tc.wantBad)
			}
		})
	}
}

func TestIdentityValidateAllFieldErrorsAreReachable(t *testing.T) {
	id := spec.Identity{Name: "Bad", Owner: "a/b", Repo: "..", EnvPrefix: "a__b", DirName: ".hidden"}
	err := id.Validate()
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("error %T does not join its causes", err)
	}
	want := map[string]string{
		"Name": "Bad", "Owner": "a/b", "Repo": "..", "EnvPrefix": "a__b", "DirName": ".hidden",
	}
	for _, cause := range joined.Unwrap() {
		var fe *validate.FieldError
		if !errors.As(cause, &fe) {
			t.Fatalf("cause %T is not a *validate.FieldError", cause)
		}
		if v, ok := want[fe.Field]; !ok || v != fe.Value {
			t.Errorf("FieldError{%q, %q} is unexpected", fe.Field, fe.Value)
		}
		delete(want, fe.Field)
	}
	if len(want) != 0 {
		t.Errorf("no FieldError for %v", want)
	}
}

func TestIdentityEnvVar(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		suffix    string
		want      string
		wantErr   error
		wantInErr string
	}{
		{name: "prefix and suffix are joined by an underscore", prefix: "ATK", suffix: "NO_COLOR", want: "ATK_NO_COLOR"},
		{name: "lowercase suffix", prefix: "ATK", suffix: "no-color", wantErr: validate.ErrInvalid, wantInErr: "suffix"},
		{name: "empty suffix", prefix: "ATK", suffix: "", wantErr: validate.ErrEmpty, wantInErr: "suffix"},
		{name: "empty prefix", prefix: "", suffix: "NO_COLOR", wantErr: validate.ErrEmpty, wantInErr: "prefix"},
		{name: "invalid prefix", prefix: "atk", suffix: "NO_COLOR", wantErr: validate.ErrInvalid, wantInErr: "prefix"},
		{
			name:   "joined name over the length limit",
			prefix: strings.Repeat("A", spec.MaxEnvPrefixLen), suffix: strings.Repeat("B", 40),
			wantErr: validate.ErrInvalid, wantInErr: "name",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := validIdentity()
			id.EnvPrefix = tc.prefix
			got, err := id.EnvVar(tc.suffix)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("EnvVar(%q) error = %v, want nil", tc.suffix, err)
				}
				if got != tc.want {
					t.Fatalf("EnvVar(%q) = %q, want %q", tc.suffix, got, tc.want)
				}
				return
			}
			if got != "" {
				t.Errorf("EnvVar(%q) = %q on failure, want empty", tc.suffix, got)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("EnvVar(%q) error = %v, want %v", tc.suffix, err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantInErr)
			}
		})
	}
}

func TestIdentityEnvVarNeverReadsTheEnvironment(t *testing.T) {
	t.Setenv("ATK_NO_COLOR", "from-the-environment")
	got, err := validIdentity().EnvVar("NO_COLOR")
	if err != nil {
		t.Fatalf("EnvVar error = %v", err)
	}
	if got != "ATK_NO_COLOR" {
		t.Fatalf("EnvVar = %q, want the variable name, not its value", got)
	}
}

func TestIdentitySlug(t *testing.T) {
	tests := []struct {
		name string
		id   spec.Identity
		want string
	}{
		{"owner and repo", validIdentity(), "pedromvgomes/agentic-toolkit"},
		{"invalid parts are not validated", spec.Identity{Owner: "A", Repo: "b"}, "A/b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.Slug(); got != tc.want {
				t.Fatalf("Slug() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIdentitySubdir(t *testing.T) {
	tests := []struct {
		name         string
		dirName      string
		sub          string
		want         string
		wantErr      error
		wantMentions []string
	}{
		{name: "name under DirName", dirName: "agentic-toolkit", sub: "cache", want: "agentic-toolkit/cache"},
		{name: "name with dots", dirName: "agentic-toolkit", sub: "v1.2", want: "agentic-toolkit/v1.2"},
		{name: "parent traversal", dirName: "agentic-toolkit", sub: "../x", wantErr: validate.ErrInvalid, wantMentions: []string{"name"}},
		{name: "two segments", dirName: "agentic-toolkit", sub: "a/b", wantErr: validate.ErrInvalid, wantMentions: []string{"name"}},
		{name: "empty name", dirName: "agentic-toolkit", sub: "", wantErr: validate.ErrEmpty, wantMentions: []string{"name"}},
		{name: "dot-dot name", dirName: "agentic-toolkit", sub: "..", wantErr: validate.ErrInvalid, wantMentions: []string{"name"}},
		{name: "dot name", dirName: "agentic-toolkit", sub: ".", wantErr: validate.ErrInvalid, wantMentions: []string{"name"}},
		{name: "invalid DirName with valid name", dirName: ".hidden", sub: "cache", wantErr: validate.ErrInvalid, wantMentions: []string{"DirName"}},
		{name: "empty DirName with valid name", dirName: "", sub: "cache", wantErr: validate.ErrEmpty, wantMentions: []string{"DirName"}},
		{name: "invalid DirName and invalid name", dirName: "a/b", sub: "../x", wantErr: validate.ErrInvalid, wantMentions: []string{"DirName", "name"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := validIdentity()
			id.DirName = tc.dirName
			got, err := id.Subdir(tc.sub)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Subdir(%q) error = %v, want nil", tc.sub, err)
				}
				if got != tc.want {
					t.Fatalf("Subdir(%q) = %q, want %q", tc.sub, got, tc.want)
				}
				return
			}
			if got != "" {
				t.Errorf("Subdir(%q) = %q on failure, want empty", tc.sub, got)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Subdir(%q) error = %v, want %v", tc.sub, err, tc.wantErr)
			}
			for _, m := range tc.wantMentions {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("error %q does not mention %q", err, m)
				}
			}
		})
	}
}

func TestIdentityIsComparable(t *testing.T) {
	a, b := validIdentity(), validIdentity()
	if a != b {
		t.Fatal("identical identities are not equal")
	}
	b.Name = "other"
	if a == b {
		t.Fatal("different identities are equal")
	}

	byIdentity := map[spec.Identity]string{a: "a", b: "b"}
	if got := byIdentity[validIdentity()]; got != "a" {
		t.Fatalf("map lookup by identity = %q, want %q", got, "a")
	}
}

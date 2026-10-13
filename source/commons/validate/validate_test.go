package validate_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"go.vipengele.org/commons/validate"
)

type outcome int

const (
	accepted outcome = iota
	rejectedEmpty
	rejectedInvalid
)

type checkCase struct {
	name  string
	value string
	want  outcome
}

// runChecks runs check over every case and asserts the outcome, the *FieldError contents and
// that the error text names the field and the quoted value.
func runChecks(t *testing.T, check func(field, value string) error, cases []checkCase) {
	t.Helper()
	const field = "the-field"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := check(field, tc.value)
			switch tc.want {
			case accepted:
				if err != nil {
					t.Fatalf("value %q: got error %v, want nil", tc.value, err)
				}
				return
			case rejectedEmpty:
				if !errors.Is(err, validate.ErrEmpty) {
					t.Fatalf("value %q: got %v, want ErrEmpty", tc.value, err)
				}
				if errors.Is(err, validate.ErrInvalid) {
					t.Fatalf("value %q: empty value must not match ErrInvalid", tc.value)
				}
			case rejectedInvalid:
				if !errors.Is(err, validate.ErrInvalid) {
					t.Fatalf("value %q: got %v, want ErrInvalid", tc.value, err)
				}
				if errors.Is(err, validate.ErrEmpty) {
					t.Fatalf("value %q: invalid value must not match ErrEmpty", tc.value)
				}
			}
			var fe *validate.FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("value %q: error %T is not a *FieldError", tc.value, err)
			}
			if fe.Field != field {
				t.Errorf("Field = %q, want %q", fe.Field, field)
			}
			if fe.Value != tc.value {
				t.Errorf("Value = %q, want %q unchanged", fe.Value, tc.value)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("error text %q does not contain the field name", err.Error())
			}
			if !strings.Contains(err.Error(), strconv.Quote(tc.value)) {
				t.Errorf("error text %q does not contain the quoted value", err.Error())
			}
		})
	}
}

// unsafeCases are rejected by every check: they hold a separator, NUL, whitespace or a
// non-ASCII byte around the letter l, which every check accepts on its own.
func unsafeCases(l string) []checkCase {
	return []checkCase{
		{"slash inside", l + "/" + l, rejectedInvalid},
		{"backslash inside", l + `\` + l, rejectedInvalid},
		{"NUL inside", l + "\x00" + l, rejectedInvalid},
		{"space inside", l + " " + l, rejectedInvalid},
		{"tab inside", l + "\t" + l, rejectedInvalid},
		{"newline inside", l + "\n" + l, rejectedInvalid},
		{"trailing newline", l + l + "\n", rejectedInvalid},
		{"leading space is not trimmed", " " + l + l, rejectedInvalid},
		{"trailing space is not trimmed", l + l + " ", rejectedInvalid},
		{"non-ASCII letter", l + "é", rejectedInvalid},
		{"non-ASCII first", "é" + l, rejectedInvalid},
		{"non-ASCII only", "é", rejectedInvalid},
		{"invalid UTF-8 byte", l + "\xff", rejectedInvalid},
	}
}

func TestSlug(t *testing.T) {
	cases := []checkCase{
		{"empty", "", rejectedEmpty},
		{"single letter", "a", accepted},
		{"letters digits and hyphens", "my-app-2", accepted},
		{"hyphen then digit", "a-1", accepted},
		{"starts with digit", "1abc", rejectedInvalid},
		{"starts with hyphen", "-abc", rejectedInvalid},
		{"starts with underscore", "_abc", rejectedInvalid},
		{"starts with dot", ".abc", rejectedInvalid},
		{"ends with hyphen", "abc-", rejectedInvalid},
		{"only a hyphen", "-", rejectedInvalid},
		{"uppercase first", "Abc", rejectedInvalid},
		{"uppercase inside", "aBc", rejectedInvalid},
		{"all uppercase is not lowered", "ABC", rejectedInvalid},
		{"underscore inside", "a_b", rejectedInvalid},
		{"dot inside", "a.b", rejectedInvalid},
		{"dot", ".", rejectedInvalid},
		{"double dot", "..", rejectedInvalid},
		{"length at the bound", strings.Repeat("a", validate.MaxSlugLen), accepted},
		{"length one over the bound", strings.Repeat("a", validate.MaxSlugLen+1), rejectedInvalid},
	}
	cases = append(cases, unsafeCases("a")...)
	runChecks(t, validate.Slug, cases)
}

func TestGitHubOwner(t *testing.T) {
	cases := []checkCase{
		{"empty", "", rejectedEmpty},
		{"single letter", "a", accepted},
		{"single digit", "7", accepted},
		{"mixed case letters digits and hyphens", "Vipengele-Org-9", accepted},
		{"uppercase only", "ABC", accepted},
		{"last letter of the alphabet in uppercase", "Z", accepted},
		{"starts with digit", "1abc", accepted},
		{"starts with hyphen", "-abc", rejectedInvalid},
		{"ends with hyphen", "abc-", rejectedInvalid},
		{"only a hyphen", "-", rejectedInvalid},
		{"underscore", "a_b", rejectedInvalid},
		{"dot", "a.b", rejectedInvalid},
		{"length at the bound", strings.Repeat("a", validate.MaxGitHubOwnerLen), accepted},
		{"length one over the bound", strings.Repeat("a", validate.MaxGitHubOwnerLen+1), rejectedInvalid},
	}
	cases = append(cases, unsafeCases("a")...)
	runChecks(t, validate.GitHubOwner, cases)
}

func TestRepoName(t *testing.T) {
	cases := []checkCase{
		{"empty", "", rejectedEmpty},
		{"single letter", "a", accepted},
		{"mixed case with separators", "My_Repo-1.x", accepted},
		{"last letter of the alphabet in uppercase", "Z", accepted},
		{"starts with hyphen", "-repo", accepted},
		{"starts with underscore", "_repo", accepted},
		{"starts with dot", ".github", accepted},
		{"ends with hyphen", "repo-", accepted},
		{"ends with underscore", "repo_", accepted},
		{"doubled underscore", "re__po", accepted},
		{"contains git without a leading dot", "mygit", accepted},
		{"contains .git in the middle", "a.git.b", accepted},
		{"dot", ".", rejectedInvalid},
		{"double dot", "..", rejectedInvalid},
		{"triple dot", "...", accepted},
		{"dot git itself", ".git", rejectedInvalid},
		{"dot git suffix", "repo.git", rejectedInvalid},
		{"double dot git suffix", "repo..git", rejectedInvalid},
		{"uppercase dot GIT suffix", "repo.GIT", accepted},
		{"length at the bound", strings.Repeat("a", validate.MaxRepoNameLen), accepted},
		{"length one over the bound", strings.Repeat("a", validate.MaxRepoNameLen+1), rejectedInvalid},
		{"length at the bound ending in dot git is rejected",
			strings.Repeat("a", validate.MaxRepoNameLen-4) + ".git", rejectedInvalid},
	}
	cases = append(cases, unsafeCases("a")...)
	runChecks(t, validate.RepoName, cases)
}

func TestEnvVar(t *testing.T) {
	cases := []checkCase{
		{"empty", "", rejectedEmpty},
		{"single letter", "A", accepted},
		{"last letter of the alphabet", "Z", accepted},
		{"letters digits and underscores", "MY_APP_2", accepted},
		{"underscore then digit", "A_1", accepted},
		{"starts with digit", "1ABC", rejectedInvalid},
		{"starts with underscore", "_ABC", rejectedInvalid},
		{"starts with hyphen", "-ABC", rejectedInvalid},
		{"starts with lowercase", "aBC", rejectedInvalid},
		{"lowercase inside", "AbC", rejectedInvalid},
		{"all lowercase is not upper-cased", "abc", rejectedInvalid},
		{"hyphen inside", "NO-COLOR", rejectedInvalid},
		{"dot inside", "A.B", rejectedInvalid},
		{"doubled underscore", "A__B", rejectedInvalid},
		{"tripled underscore", "A___B", rejectedInvalid},
		{"ends with underscore", "ABC_", rejectedInvalid},
		{"ends with doubled underscore", "ABC__", rejectedInvalid},
		{"only an underscore", "_", rejectedInvalid},
		{"dot", ".", rejectedInvalid},
		{"double dot", "..", rejectedInvalid},
		{"length at the bound", strings.Repeat("A", validate.MaxEnvVarLen), accepted},
		{"length one over the bound", strings.Repeat("A", validate.MaxEnvVarLen+1), rejectedInvalid},
	}
	cases = append(cases, unsafeCases("A")...)
	runChecks(t, validate.EnvVar, cases)
}

func TestPathSegment(t *testing.T) {
	cases := []checkCase{
		{"empty", "", rejectedEmpty},
		{"single letter", "a", accepted},
		{"single digit", "0", accepted},
		{"letters digits and separators", "my_file-1.tar.gz", accepted},
		{"starts with digit", "1abc", accepted},
		{"starts with hyphen", "-abc", rejectedInvalid},
		{"starts with underscore", "_abc", rejectedInvalid},
		{"starts with dot", ".hidden", rejectedInvalid},
		{"dot", ".", rejectedInvalid},
		{"double dot", "..", rejectedInvalid},
		{"triple dot", "...", rejectedInvalid},
		{"dot git", ".git", rejectedInvalid},
		{"ends with dot", "abc.", accepted},
		{"ends with hyphen", "abc-", accepted},
		{"ends with underscore", "abc_", accepted},
		{"doubled underscore", "a__b", accepted},
		{"uppercase first", "Abc", rejectedInvalid},
		{"uppercase inside", "aBc", rejectedInvalid},
		{"all uppercase is not lowered", "ABC", rejectedInvalid},
		{"parent traversal", "../etc", rejectedInvalid},
		{"traversal after a segment", "a/../b", rejectedInvalid},
		{"length at the bound", strings.Repeat("a", validate.MaxPathSegmentLen), accepted},
		{"length one over the bound", strings.Repeat("a", validate.MaxPathSegmentLen+1), rejectedInvalid},
	}
	cases = append(cases, unsafeCases("a")...)
	runChecks(t, validate.PathSegment, cases)
}

func TestFieldError(t *testing.T) {
	t.Run("error text has the field, the quoted value and the cause", func(t *testing.T) {
		err := validate.Slug("project", "Bad Value")
		want := `project "Bad Value": value is invalid`
		if !strings.HasPrefix(err.Error(), want) {
			t.Fatalf("error text = %q, want prefix %q", err.Error(), want)
		}
	})
	t.Run("empty value error names the field and cause", func(t *testing.T) {
		err := validate.Slug("project", "")
		if got, want := err.Error(), `project "": value is empty`; got != want {
			t.Fatalf("error text = %q, want %q", got, want)
		}
	})
	t.Run("value with control characters is quoted", func(t *testing.T) {
		err := validate.Slug("project", "a\nb")
		if !strings.Contains(err.Error(), `"a\nb"`) {
			t.Fatalf("error text %q does not contain the escaped value", err.Error())
		}
	})
	t.Run("unwrap returns the cause", func(t *testing.T) {
		fe := &validate.FieldError{Field: "f", Value: "v", Err: validate.ErrInvalid}
		if fe.Unwrap() != validate.ErrInvalid {
			t.Fatalf("Unwrap() = %v, want ErrInvalid", fe.Unwrap())
		}
	})
}

func TestJoin(t *testing.T) {
	t.Run("no arguments yields nil", func(t *testing.T) {
		if err := validate.Join(); err != nil {
			t.Fatalf("Join() = %v, want nil", err)
		}
	})
	t.Run("all passing checks yield nil", func(t *testing.T) {
		err := validate.Join(
			validate.Slug("slug", "abc"),
			validate.EnvVar("env", "ABC"),
			validate.PathSegment("segment", "abc"),
		)
		if err != nil {
			t.Fatalf("Join = %v, want nil", err)
		}
	})
	t.Run("one failure keeps its field and sentinel", func(t *testing.T) {
		err := validate.Join(validate.Slug("slug", "abc"), validate.EnvVar("env", "abc"))
		if !errors.Is(err, validate.ErrInvalid) || errors.Is(err, validate.ErrEmpty) {
			t.Fatalf("Join = %v, want only ErrInvalid", err)
		}
		if !strings.Contains(err.Error(), "env") || strings.Contains(err.Error(), "slug") {
			t.Fatalf("error text %q must name only the failing field", err.Error())
		}
	})
	t.Run("several failures mention every failing field and match both sentinels", func(t *testing.T) {
		err := validate.Join(
			validate.Slug("slug", ""),
			validate.GitHubOwner("owner", "-bad"),
			validate.RepoName("repo", ".."),
			validate.EnvVar("env", "ok"),
			validate.PathSegment("segment", "fine"),
		)
		if err == nil {
			t.Fatal("Join = nil, want an error")
		}
		for _, field := range []string{"slug", "owner", "repo", "env"} {
			if !strings.Contains(err.Error(), field) {
				t.Errorf("error text %q does not mention %q", err.Error(), field)
			}
		}
		if strings.Contains(err.Error(), "segment") {
			t.Errorf("error text %q mentions a passing field", err.Error())
		}
		if !errors.Is(err, validate.ErrEmpty) {
			t.Error("errors.Is(err, ErrEmpty) = false through the joined error")
		}
		if !errors.Is(err, validate.ErrInvalid) {
			t.Error("errors.Is(err, ErrInvalid) = false through the joined error")
		}
	})
	t.Run("errors.As finds each FieldError", func(t *testing.T) {
		first := validate.Slug("first", "Bad")
		second := validate.EnvVar("second", "bad")
		err := validate.Join(first, second)
		var target *validate.FieldError
		if !errors.As(err, &target) {
			t.Fatal("errors.As found no *FieldError")
		}
		if target.Field != "first" {
			t.Errorf("errors.As found field %q first, want %q", target.Field, "first")
		}
		for _, want := range []error{first, second} {
			if !errors.Is(err, want) {
				t.Errorf("joined error does not contain %v", want)
			}
		}
		type multi interface{ Unwrap() []error }
		m, ok := err.(multi)
		if !ok {
			t.Fatalf("joined error %T does not expose its members", err)
		}
		fields := map[string]string{}
		for _, member := range m.Unwrap() {
			var fe *validate.FieldError
			if !errors.As(member, &fe) {
				t.Fatalf("member %v is not a *FieldError", member)
			}
			fields[fe.Field] = fe.Value
		}
		if fields["first"] != "Bad" || fields["second"] != "bad" || len(fields) != 2 {
			t.Errorf("members = %v, want first=Bad and second=bad", fields)
		}
	})
}

// checkFuzzProperty asserts the invariants every check holds for any input.
func checkFuzzProperty(t *testing.T, check func(field, value string) error, maxLen int, value string) {
	t.Helper()
	const field = "fuzz"
	err := check(field, value)
	if err == nil {
		if value == "" {
			t.Fatal("empty value accepted")
		}
		if len(value) > maxLen {
			t.Fatalf("accepted %q of %d bytes, bound is %d", value, len(value), maxLen)
		}
		for i := 0; i < len(value); i++ {
			c := value[i]
			if c >= 0x80 {
				t.Fatalf("accepted %q holding non-ASCII byte %#x", value, c)
			}
			if c == '/' || c == '\\' || c == 0 {
				t.Fatalf("accepted %q holding forbidden byte %#x", value, c)
			}
		}
		for _, r := range value {
			if unicode.IsSpace(r) {
				t.Fatalf("accepted %q holding whitespace %U", value, r)
			}
		}
		return
	}
	var fe *validate.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("rejection of %q is %T, not a *FieldError", value, err)
	}
	if fe.Field != field || fe.Value != value {
		t.Fatalf("FieldError = {%q, %q}, want {%q, %q}", fe.Field, fe.Value, field, value)
	}
	isEmpty, isInvalid := errors.Is(err, validate.ErrEmpty), errors.Is(err, validate.ErrInvalid)
	if isEmpty == isInvalid {
		t.Fatalf("rejection of %q must wrap exactly one of ErrEmpty and ErrInvalid", value)
	}
	if isEmpty != (value == "") {
		t.Fatalf("rejection of %q: ErrEmpty = %v", value, isEmpty)
	}
}

func addSeeds(f *testing.F) {
	f.Helper()
	for _, seed := range []string{
		"", "a", "A", "0", "abc-def", "ABC_DEF", "Abc", "a-", "-a", "a_", "_a", "A__B",
		".", "..", ".git", "repo.git", "a/b", `a\b`, "a\x00b", "a b", "a\tb", "a\nb", "é",
		"\xff", "my_file-1.tar.gz", strings.Repeat("a", 64), strings.Repeat("a", 65),
		strings.Repeat("A", 100),
	} {
		f.Add(seed)
	}
}

func FuzzSlug(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, value string) {
		checkFuzzProperty(t, validate.Slug, validate.MaxSlugLen, value)
	})
}

func FuzzGitHubOwner(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, value string) {
		checkFuzzProperty(t, validate.GitHubOwner, validate.MaxGitHubOwnerLen, value)
	})
}

func FuzzRepoName(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, value string) {
		checkFuzzProperty(t, validate.RepoName, validate.MaxRepoNameLen, value)
	})
}

func FuzzEnvVar(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, value string) {
		checkFuzzProperty(t, validate.EnvVar, validate.MaxEnvVarLen, value)
	})
}

func FuzzPathSegment(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, value string) {
		checkFuzzProperty(t, validate.PathSegment, validate.MaxPathSegmentLen, value)
	})
}

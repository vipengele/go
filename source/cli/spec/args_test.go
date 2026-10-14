package spec_test

import (
	"errors"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/commons/validate"
)

func argsOf(n int) []string {
	return make([]string, n)
}

func TestArgPolicyCheck(t *testing.T) {
	tests := []struct {
		name   string
		policy spec.ArgPolicy
		accept []int
		reject []int
	}{
		{"zero value accepts no arguments", spec.ArgPolicy{}, []int{0}, []int{1, 2}},
		{"NoArgs", spec.NoArgs(), []int{0}, []int{1, 5}},
		{"AnyArgs", spec.AnyArgs(), []int{0, 1, 100}, nil},
		{"ExactArgs(0)", spec.ExactArgs(0), []int{0}, []int{1}},
		{"ExactArgs(2)", spec.ExactArgs(2), []int{2}, []int{0, 1, 3}},
		{"RangeArgs(1, 3)", spec.RangeArgs(1, 3), []int{1, 2, 3}, []int{0, 4}},
		{"RangeArgs(2, 2)", spec.RangeArgs(2, 2), []int{2}, []int{1, 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.policy.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			for _, n := range tc.accept {
				if err := tc.policy.Check(argsOf(n)); err != nil {
					t.Errorf("Check(%d args) = %v, want nil", n, err)
				}
			}
			for _, n := range tc.reject {
				err := tc.policy.Check(argsOf(n))
				var ace *spec.ArgCountError
				if !errors.As(err, &ace) {
					t.Errorf("Check(%d args) = %v, want an *ArgCountError", n, err)
					continue
				}
				if ace.Got != n {
					t.Errorf("ArgCountError.Got = %d, want %d", ace.Got, n)
				}
				if ace.Policy != tc.policy {
					t.Errorf("ArgCountError.Policy = %v, want %v", ace.Policy, tc.policy)
				}
			}
		})
	}
}

func TestArgPolicyZeroValueIsNoArgs(t *testing.T) {
	if (spec.ArgPolicy{}) != spec.NoArgs() {
		t.Fatal("ArgPolicy{} != NoArgs()")
	}
	var c spec.Command
	if err := c.Args.Check([]string{"x"}); err == nil {
		t.Fatal("zero Command.Args accepted an argument")
	}
}

func TestArgPolicyBounds(t *testing.T) {
	tests := []struct {
		name        string
		policy      spec.ArgPolicy
		wantMin     int
		wantMax     int
		wantBounded bool
	}{
		{"NoArgs", spec.NoArgs(), 0, 0, true},
		{"AnyArgs", spec.AnyArgs(), 0, 0, false},
		{"ExactArgs", spec.ExactArgs(3), 3, 3, true},
		{"RangeArgs", spec.RangeArgs(1, 4), 1, 4, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.Min(); got != tc.wantMin {
				t.Errorf("Min() = %d, want %d", got, tc.wantMin)
			}
			gotMax, bounded := tc.policy.Max()
			if bounded != tc.wantBounded || gotMax != tc.wantMax {
				t.Errorf("Max() = %d, %t, want %d, %t", gotMax, bounded, tc.wantMax, tc.wantBounded)
			}
		})
	}
}

func TestArgPolicyValidateRejectsInvalidBounds(t *testing.T) {
	tests := []struct {
		name   string
		policy spec.ArgPolicy
	}{
		{"negative ExactArgs", spec.ExactArgs(-1)},
		{"negative RangeArgs minimum", spec.RangeArgs(-1, 2)},
		{"negative RangeArgs maximum", spec.RangeArgs(0, -2)},
		{"RangeArgs minimum above maximum", spec.RangeArgs(3, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if !errors.Is(err, validate.ErrInvalid) {
				t.Fatalf("Validate() = %v, want ErrInvalid", err)
			}
			checkErr := tc.policy.Check(nil)
			if !errors.Is(checkErr, validate.ErrInvalid) {
				t.Fatalf("Check() = %v, want ErrInvalid", checkErr)
			}
			var ace *spec.ArgCountError
			if errors.As(checkErr, &ace) {
				t.Fatalf("Check() = %v, an *ArgCountError for an invalid policy", checkErr)
			}
		})
	}
}

func TestArgCountErrorMessage(t *testing.T) {
	tests := []struct {
		policy spec.ArgPolicy
		n      int
		want   string
	}{
		{spec.NoArgs(), 2, "accepts no arguments, received 2 arguments"},
		{spec.ExactArgs(1), 0, "accepts exactly 1 argument, received 0 arguments"},
		{spec.ExactArgs(2), 1, "accepts exactly 2 arguments, received 1 argument"},
		{spec.RangeArgs(1, 3), 4, "accepts between 1 and 3 arguments, received 4 arguments"},
	}
	for _, tc := range tests {
		err := tc.policy.Check(argsOf(tc.n))
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v.Check(%d args) = %v, want %q", tc.policy, tc.n, err, tc.want)
		}
	}
}

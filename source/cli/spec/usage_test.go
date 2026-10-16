package spec_test

import (
	"errors"
	"fmt"
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestUsageErrorText(t *testing.T) {
	tests := []struct {
		name string
		err  *spec.UsageError
		want string
	}{
		{
			name: "with a flag",
			err:  &spec.UsageError{Question: "delete 3 files?", Flag: "--yes"},
			want: "delete 3 files? requires interactive input; pass --yes",
		},
		{
			name: "flag printed as given",
			err:  &spec.UsageError{Question: "name", Flag: "-n NAME"},
			want: "name requires interactive input; pass -n NAME",
		},
		{
			name: "without a flag",
			err:  &spec.UsageError{Question: "delete 3 files?"},
			want: "delete 3 files? requires interactive input",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUsageErrorFoundThroughAWrap(t *testing.T) {
	want := &spec.UsageError{Question: "continue?", Flag: "--yes"}
	var got *spec.UsageError
	if !errors.As(fmt.Errorf("deploy: %w", want), &got) || got != want {
		t.Errorf("errors.As found %v, want %v", got, want)
	}
}

func TestUsageErrorWrapsNothing(t *testing.T) {
	if inner := errors.Unwrap(&spec.UsageError{Question: "continue?"}); inner != nil {
		t.Errorf("errors.Unwrap = %v, want nil", inner)
	}
}

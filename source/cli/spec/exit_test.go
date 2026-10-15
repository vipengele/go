package spec_test

import (
	"errors"
	"fmt"
	"testing"

	"go.vipengele.org/cli/spec"
)

var errBase = errors.New("base failure")

func codeOf(t *testing.T, err error) (int, bool) {
	t.Helper()
	var ce *spec.CodedError
	if !errors.As(err, &ce) {
		return 0, false
	}
	return ce.Code(), true
}

func isReported(err error) bool {
	var re *spec.ReportedError
	return errors.As(err, &re)
}

func TestCodedNilIsNil(t *testing.T) {
	if err := spec.Coded(3, nil); err != nil {
		t.Errorf("Coded(3, nil) = %v, want nil", err)
	}
}

func TestReportedNilIsNil(t *testing.T) {
	if err := spec.Reported(nil); err != nil {
		t.Errorf("Reported(nil) = %v, want nil", err)
	}
}

func TestCodedKeepsTextAndUnwraps(t *testing.T) {
	err := spec.Coded(7, errBase)
	if err.Error() != errBase.Error() {
		t.Errorf("Error() = %q, want %q", err.Error(), errBase.Error())
	}
	if !errors.Is(err, errBase) {
		t.Error("errors.Is(Coded(...), base) = false, want true")
	}
	if code, ok := codeOf(t, err); !ok || code != 7 {
		t.Errorf("code = %d, %v, want 7, true", code, ok)
	}
}

func TestReportedKeepsTextAndUnwraps(t *testing.T) {
	err := spec.Reported(errBase)
	if err.Error() != errBase.Error() {
		t.Errorf("Error() = %q, want %q", err.Error(), errBase.Error())
	}
	if !errors.Is(err, errBase) {
		t.Error("errors.Is(Reported(...), base) = false, want true")
	}
	if !isReported(err) {
		t.Error("errors.As(Reported(...), *ReportedError) = false, want true")
	}
}

type innerError struct{ name string }

func (e *innerError) Error() string { return e.name }

func TestAsReachesInnerError(t *testing.T) {
	inner := &innerError{name: "inner"}
	for name, err := range map[string]error{
		"coded":    spec.Coded(2, inner),
		"reported": spec.Reported(inner),
	} {
		var got *innerError
		if !errors.As(err, &got) || got != inner {
			t.Errorf("%s: errors.As did not reach the inner error", name)
		}
	}
}

func TestWrappedThroughWrapAndJoin(t *testing.T) {
	coded := spec.Coded(4, errBase)
	reported := spec.Reported(errBase)
	tests := []struct {
		name         string
		err          error
		wantCode     int
		wantCoded    bool
		wantReported bool
	}{
		{"coded through %w", fmt.Errorf("run: %w", coded), 4, true, false},
		{"coded through Join", errors.Join(errors.New("other"), coded), 4, true, false},
		{"reported through %w", fmt.Errorf("run: %w", reported), 0, false, true},
		{"reported through Join", errors.Join(errors.New("other"), reported), 0, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, errBase) {
				t.Error("errors.Is(err, base) = false, want true")
			}
			code, ok := codeOf(t, tc.err)
			if ok != tc.wantCoded || code != tc.wantCode {
				t.Errorf("code = %d, %v, want %d, %v", code, ok, tc.wantCode, tc.wantCoded)
			}
			if got := isReported(tc.err); got != tc.wantReported {
				t.Errorf("reported = %v, want %v", got, tc.wantReported)
			}
		})
	}
}

func TestReportedAndCodedCompose(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"reported around coded", spec.Reported(spec.Coded(5, errBase))},
		{"coded around reported", spec.Coded(5, spec.Reported(errBase))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if code, ok := codeOf(t, tc.err); !ok || code != 5 {
				t.Errorf("code = %d, %v, want 5, true", code, ok)
			}
			if !isReported(tc.err) {
				t.Error("reported = false, want true")
			}
			if !errors.Is(tc.err, errBase) {
				t.Error("errors.Is(err, base) = false, want true")
			}
			if tc.err.Error() != errBase.Error() {
				t.Errorf("Error() = %q, want %q", tc.err.Error(), errBase.Error())
			}
		})
	}
}

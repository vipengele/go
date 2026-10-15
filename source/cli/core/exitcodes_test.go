package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type exitTestError struct{ code int }

func (e *exitTestError) Error() string { return fmt.Sprintf("exit test error %d", e.code) }

func TestSentinelMatcherRefusesUnregistrableCodes(t *testing.T) {
	for _, code := range []int{0, ExitUsage, ExitInterrupted, -1, 126, 255} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			defer func() {
				msg, _ := recover().(string)
				if msg == "" {
					t.Fatalf("code %d: no panic", code)
				}
				if want := fmt.Sprintf("exit code %d ", code); !strings.Contains(msg, want) {
					t.Fatalf("panic %q does not name the code (%q)", msg, want)
				}
			}()
			sentinelMatcher(code, errors.New("x"))
		})
	}
}

func TestSentinelMatcherAcceptsRegistrableCodes(t *testing.T) {
	sentinel := errors.New("x")
	for _, code := range []int{1, 3, 64, 125} {
		got, ok := sentinelMatcher(code, sentinel)(fmt.Errorf("wrapped: %w", sentinel))
		if !ok || got != code {
			t.Errorf("code %d: got (%d, %v)", code, got, ok)
		}
	}
}

func TestExitMatchersIs(t *testing.T) {
	sentinel := errors.New("sentinel")
	m := exitMatchers{sentinelMatcher(7, sentinel)}
	if got, ok := m.code(fmt.Errorf("w: %w", sentinel)); !ok || got != 7 {
		t.Fatalf("got (%d, %v), want (7, true)", got, ok)
	}
}

func TestExitMatchersAs(t *testing.T) {
	m := exitMatchers{typedMatcher(func(e *exitTestError) int { return e.code })}
	if got, ok := m.code(fmt.Errorf("w: %w", &exitTestError{code: 9})); !ok || got != 9 {
		t.Fatalf("got (%d, %v), want (9, true)", got, ok)
	}
}

func TestExitMatchersRegistrationOrderBreaksTies(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	m := exitMatchers{sentinelMatcher(10, a), sentinelMatcher(20, b)}
	for _, err := range []error{errors.Join(a, b), errors.Join(b, a)} {
		if got, ok := m.code(err); !ok || got != 10 {
			t.Errorf("%v: got (%d, %v), want (10, true)", err, got, ok)
		}
	}
}

func TestExitMatchersOrderAcrossKinds(t *testing.T) {
	sentinel := errors.New("s")
	err := errors.Join(sentinel, &exitTestError{code: 5})
	typed := typedMatcher(func(e *exitTestError) int { return e.code })
	if got, _ := (exitMatchers{typed, sentinelMatcher(6, sentinel)}).code(err); got != 5 {
		t.Errorf("typed first: got %d, want 5", got)
	}
	if got, _ := (exitMatchers{sentinelMatcher(6, sentinel), typed}).code(err); got != 6 {
		t.Errorf("sentinel first: got %d, want 6", got)
	}
}

func TestTypedMatcherIgnoresUnregistrableCodes(t *testing.T) {
	for _, code := range []int{0, ExitUsage, ExitInterrupted, -1, 126} {
		m := exitMatchers{typedMatcher(func(*exitTestError) int { return code })}
		if got, ok := m.code(&exitTestError{}); ok || got != 0 {
			t.Errorf("code %d: got (%d, %v), want (0, false)", code, got, ok)
		}
	}
}

func TestExitMatchersNoMatch(t *testing.T) {
	m := exitMatchers{sentinelMatcher(7, errors.New("s"))}
	if got, ok := m.code(errors.New("other")); ok || got != 0 {
		t.Errorf("got (%d, %v), want (0, false)", got, ok)
	}
	if got, ok := m.code(nil); ok || got != 0 {
		t.Errorf("nil error: got (%d, %v), want (0, false)", got, ok)
	}
	if got, ok := (exitMatchers(nil)).code(errors.New("e")); ok || got != 0 {
		t.Errorf("empty list: got (%d, %v), want (0, false)", got, ok)
	}
}

func TestMatchersReturnCodeZeroWhenTheyDoNotMatch(t *testing.T) {
	other := errors.New("other")
	cases := map[string]exitMatcher{
		"sentinel":                sentinelMatcher(7, errors.New("s")),
		"typed, no target":        typedMatcher(func(*exitTestError) int { return 7 }),
		"typed, unregistrable":    typedMatcher(func(*exitTestError) int { return ExitInterrupted }),
		"typed, wrong error type": typedMatcher(func(*exitTestError) int { return 8 }),
	}
	for name, match := range cases {
		err := error(other)
		if name == "typed, unregistrable" {
			err = &exitTestError{}
		}
		if got, ok := match(err); ok || got != 0 {
			t.Errorf("%s: got (%d, %v), want (0, false)", name, got, ok)
		}
	}
}

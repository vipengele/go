package update

import (
	"errors"
	"strings"
	"testing"
)

func TestRunPostUpdateRunsHooksInOrderWithThePath(t *testing.T) {
	var calls []string
	hook := func(name string) PostUpdate {
		return func(path string) error {
			calls = append(calls, name+"="+path)
			return nil
		}
	}

	err := RunPostUpdate([]PostUpdate{hook("a"), hook("b"), hook("c")}, "/bin/atk")
	if err != nil {
		t.Fatalf("RunPostUpdate = %v, want nil", err)
	}
	if got, want := strings.Join(calls, ","), "a=/bin/atk,b=/bin/atk,c=/bin/atk"; got != want {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

func TestRunPostUpdateJoinsErrorsAndRunsLaterHooks(t *testing.T) {
	errFirst, errThird := errors.New("first failed"), errors.New("third failed")
	ran := false
	hooks := []PostUpdate{
		func(string) error { return errFirst },
		func(string) error { ran = true; return nil },
		func(string) error { return errThird },
	}

	err := RunPostUpdate(hooks, "/bin/atk")
	if !ran {
		t.Error("a hook after a failing one did not run")
	}
	if !errors.Is(err, errFirst) || !errors.Is(err, errThird) {
		t.Errorf("err = %v, want it to wrap both hook errors", err)
	}
	for _, want := range []string{"first failed", "third failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to contain %q", err, want)
		}
	}
}

func TestRunPostUpdateReturnsNilWithoutHooks(t *testing.T) {
	if err := RunPostUpdate(nil, "/bin/atk"); err != nil {
		t.Errorf("nil list: %v", err)
	}
	if err := RunPostUpdate([]PostUpdate{}, "/bin/atk"); err != nil {
		t.Errorf("empty list: %v", err)
	}
}

func TestRunPostUpdateSkipsNilHooks(t *testing.T) {
	calls := 0
	hooks := []PostUpdate{nil, func(string) error { calls++; return nil }, nil}

	if err := RunPostUpdate(hooks, "/bin/atk"); err != nil {
		t.Fatalf("RunPostUpdate = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

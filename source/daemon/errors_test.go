package daemon

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"runner exited", ErrRunnerExited, "daemon: runner exited before shutdown"},
		{"start timeout", ErrStartTimeout, "daemon: runner start timed out"},
		{"stop timeout", ErrStopTimeout, "daemon: runner stop timed out"},
		{"already ran", ErrAlreadyRan, "daemon: supervisor already ran"},
		{"restart required", ErrRestartRequired, "daemon: restart required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSentinelsMatchThroughWrapping(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"runner exited", ErrRunnerExited},
		{"start timeout", ErrStartTimeout},
		{"stop timeout", ErrStopTimeout},
		{"already ran", ErrAlreadyRan},
	}
	for _, tt := range sentinels {
		wrappers := []struct {
			name string
			wrap func(error) error
		}{
			{"wrapped", func(e error) error { return fmt.Errorf("ctx: %w", e) }},
			{"joined", func(e error) error { return errors.Join(errors.New("other"), e) }},
		}
		for _, w := range wrappers {
			t.Run(tt.name+" "+w.name, func(t *testing.T) {
				if !errors.Is(w.wrap(tt.err), tt.err) {
					t.Errorf("errors.Is lost %v through %s", tt.err, w.name)
				}
				for _, other := range sentinels {
					if other.err != tt.err && errors.Is(w.wrap(tt.err), other.err) {
						t.Errorf("%v matches %v", tt.err, other.err)
					}
				}
			})
		}
	}
}

func TestRestartRequired(t *testing.T) {
	restart := fmt.Errorf("reload: %w", ErrRestartRequired)
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"bare", ErrRestartRequired, true},
		{"wrapped", fmt.Errorf("run: %w", ErrRestartRequired), true},
		{"wrapped twice", fmt.Errorf("outer: %w", restart), true},
		{"joined restarts only", errors.Join(ErrRestartRequired, restart), true},
		{"joined restarts inside a wrapper", fmt.Errorf("run: %w", errors.Join(ErrRestartRequired, restart)), true},
		{"joined with stop timeout", errors.Join(ErrRestartRequired, ErrStopTimeout), false},
		{"wrapped join with stop timeout", fmt.Errorf("run: %w", errors.Join(restart, ErrStopTimeout)), false},
		{"panic", &PanicError{Runner: "http", Value: "oops"}, false},
		{"panic wrapping a restart", &PanicError{Runner: "http", Value: ErrRestartRequired}, false},
		{"panic joined with a restart", errors.Join(ErrRestartRequired, &PanicError{Runner: "http", Value: "oops"}), false},
		{"wrapped panic wrapping a restart", fmt.Errorf("run: %w", &PanicError{Runner: "http", Value: ErrRestartRequired}), false},
		{"nil", nil, false},
		{"unrelated", errors.New("other"), false},
		{"wrapped unrelated", fmt.Errorf("run: %w", ErrRunnerExited), false},
		{"joined with no members", emptyJoin{}, false},
		{"wrapped join with no members", fmt.Errorf("run: %w", emptyJoin{}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RestartRequested(tt.err); got != tt.want {
				t.Errorf("RestartRequested(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// emptyJoin is a joined error with no members.
type emptyJoin struct{}

func (emptyJoin) Error() string   { return "empty" }
func (emptyJoin) Unwrap() []error { return nil }

type valueError struct{}

func (valueError) Error() string { return "boom" }

func TestPanicError(t *testing.T) {
	cause := valueError{}
	tests := []struct {
		name      string
		value     any
		wantMsg   string
		wantCause error
	}{
		{"error value", cause, `daemon: runner "http" panicked: boom`, cause},
		{"non-error value", "oops", `daemon: runner "http" panicked: oops`, nil},
		{"nil value", nil, `daemon: runner "http" panicked: <nil>`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe := &PanicError{Runner: "http", Value: tt.value, Stack: []byte("stack")}
			if got := pe.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			if got := pe.Unwrap(); got != tt.wantCause {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantCause)
			}

			wrapped := fmt.Errorf("run: %w", pe)
			var target *PanicError
			if !errors.As(wrapped, &target) || target != pe {
				t.Errorf("errors.As through %%w did not find the *PanicError")
			}
			joined := errors.Join(ErrRunnerExited, pe)
			target = nil
			if !errors.As(joined, &target) || target != pe {
				t.Errorf("errors.As through Join did not find the *PanicError")
			}
			if tt.wantCause != nil && !errors.Is(wrapped, tt.wantCause) {
				t.Errorf("errors.Is did not reach the panic value")
			}
			if tt.wantCause == nil && errors.Is(wrapped, valueError{}) {
				t.Errorf("errors.Is reached a panic value that is not an error")
			}
		})
	}
}

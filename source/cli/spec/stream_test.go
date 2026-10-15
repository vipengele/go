package spec_test

import (
	"bytes"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

// fileLike is a stream with an Fd method, as an *os.File has, without opening a file.
type fileLike struct {
	bytes.Buffer
}

func (*fileLike) Fd() uintptr { return 3 }

func allStreams() []spec.Stream { return []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} }

func TestStreamString(t *testing.T) {
	cases := map[spec.Stream]string{
		spec.Stdin:      "stdin",
		spec.Stdout:     "stdout",
		spec.Stderr:     "stderr",
		spec.Stream(7):  "Stream(7)",
		spec.Stream(-1): "Stream(-1)",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Stream(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestIsTerminalWithoutTerminalIsFalseForBuffers(t *testing.T) {
	env := &spec.Env{Stdin: strings.NewReader("in"), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	for _, s := range allStreams() {
		if env.IsTerminal(s) {
			t.Errorf("IsTerminal(%v) = true for a buffer, want false", s)
		}
	}
}

func TestIsTerminalWithoutTerminalIsFalseForNilStreams(t *testing.T) {
	for _, s := range allStreams() {
		if (&spec.Env{}).IsTerminal(s) {
			t.Errorf("IsTerminal(%v) = true for a nil stream, want false", s)
		}
	}
}

func TestIsTerminalOfNilEnvIsFalse(t *testing.T) {
	var env *spec.Env
	for _, s := range allStreams() {
		if env.IsTerminal(s) {
			t.Errorf("IsTerminal(%v) on a nil Env = true, want false", s)
		}
	}
}

func TestIsTerminalWithoutTerminalPanicsForFileLikeStream(t *testing.T) {
	for _, s := range allStreams() {
		t.Run(s.String(), func(t *testing.T) {
			env := &spec.Env{Stdin: &fileLike{}, Stdout: &fileLike{}, Stderr: &fileLike{}}
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("IsTerminal did not panic for a file-like stream with a nil Terminal")
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic value %T, want string", r)
				}
				for _, want := range []string{s.String(), "Env.Terminal is nil", "cli/core", "set Env.Terminal"} {
					if !strings.Contains(msg, want) {
						t.Errorf("panic message %q does not mention %q", msg, want)
					}
				}
			}()
			env.IsTerminal(s)
		})
	}
}

func TestIsTerminalCallsTerminalOnEveryQueryWithItsStream(t *testing.T) {
	var calls []spec.Stream
	env := &spec.Env{
		Stdin:  &fileLike{},
		Stdout: &fileLike{},
		Stderr: &bytes.Buffer{},
		Terminal: func(s spec.Stream) bool {
			calls = append(calls, s)
			return true
		},
	}
	queries := []spec.Stream{spec.Stdout, spec.Stdout, spec.Stdin, spec.Stderr, spec.Stdout}
	for _, s := range queries {
		if !env.IsTerminal(s) {
			t.Errorf("IsTerminal(%v) = false, want the injected answer true", s)
		}
	}
	if len(calls) != len(queries) {
		t.Fatalf("Terminal called %d times, want %d: %v", len(calls), len(queries), calls)
	}
	for i := range queries {
		if calls[i] != queries[i] {
			t.Errorf("call %d passed %v, want %v", i, calls[i], queries[i])
		}
	}
}

func TestIsTerminalAnswersEachStreamSeparately(t *testing.T) {
	terminals := map[spec.Stream]bool{spec.Stdin: true, spec.Stdout: false, spec.Stderr: true}
	env := &spec.Env{Terminal: func(s spec.Stream) bool { return terminals[s] }}
	for _, s := range allStreams() {
		if got := env.IsTerminal(s); got != terminals[s] {
			t.Errorf("IsTerminal(%v) = %t, want %t", s, got, terminals[s])
		}
	}
}

func TestIsTerminalOfUnknownStreamIsFalseWithoutCallingTerminal(t *testing.T) {
	called := false
	env := &spec.Env{
		Stdout:   &fileLike{},
		Terminal: func(spec.Stream) bool { called = true; return true },
	}
	if env.IsTerminal(spec.Stream(7)) {
		t.Error("IsTerminal(Stream(7)) = true, want false")
	}
	if called {
		t.Error("Terminal was called for a Stream that names no standard stream")
	}
}

func TestIsTerminalWithoutTerminalIsFalseForTypedNilFileStreams(t *testing.T) {
	var f *fileLike
	env := &spec.Env{Stdin: f, Stdout: f, Stderr: f}
	for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
		if env.IsTerminal(s) {
			t.Errorf("IsTerminal(%v) of a typed-nil file stream is true, want false", s)
		}
	}
}

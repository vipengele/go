package daemon

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct{ name string }

func (f *fakeRunner) Name() string                { return f.name }
func (f *fakeRunner) Start(context.Context) error { return nil }
func (f *fakeRunner) Stop(context.Context) error  { return nil }

func TestNewRejects(t *testing.T) {
	tests := []struct {
		name    string
		runners []Runner
		opts    []Option
	}{
		{name: "nil list"},
		{name: "empty list", runners: []Runner{}},
		{name: "nil runner", runners: []Runner{&fakeRunner{"a"}, nil}},
		{name: "empty name", runners: []Runner{&fakeRunner{""}}},
		{name: "whitespace name", runners: []Runner{&fakeRunner{" \t\n"}}},
		{name: "duplicate names", runners: []Runner{&fakeRunner{"a"}, &fakeRunner{"b"}, &fakeRunner{"a"}}},
		{name: "zero grace", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithGrace(0)}},
		{name: "negative grace", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithGrace(-time.Second)}},
		{name: "unknown policy", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithPanicPolicy(PanicPolicy(99))}},
		{name: "negative policy", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{WithPanicPolicy(PanicPolicy(-1))}},
		{name: "nil option", runners: []Runner{&fakeRunner{"a"}}, opts: []Option{nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.runners, tt.opts...)
			if err == nil {
				t.Fatalf("New succeeded, want an error")
			}
			if s != nil {
				t.Errorf("New returned a Supervisor alongside an error")
			}
		})
	}
}

func TestNewErrorNamesTheOffender(t *testing.T) {
	_, err := New([]Runner{&fakeRunner{"a"}, &fakeRunner{"a"}})
	if err == nil || !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("duplicate-name error = %v, want it to name %q", err, "a")
	}
	_, err = New([]Runner{&fakeRunner{"a"}, nil})
	if err == nil || !strings.Contains(err.Error(), "1") {
		t.Errorf("nil-runner error = %v, want it to name index 1", err)
	}
}

func TestNewAccepts(t *testing.T) {
	tests := []struct {
		name       string
		runners    []Runner
		opts       []Option
		wantLen    int
		wantGrace  time.Duration
		wantPolicy PanicPolicy
	}{
		{
			name:       "one runner, defaults",
			runners:    []Runner{&fakeRunner{"a"}},
			wantLen:    1,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "several runners",
			runners:    []Runner{&fakeRunner{"a"}, &fakeRunner{"b"}, &fakeRunner{"c"}},
			wantLen:    3,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "names differing in case or padding",
			runners:    []Runner{&fakeRunner{"a"}, &fakeRunner{"A"}, &fakeRunner{" a"}},
			wantLen:    3,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "custom grace",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithGrace(3 * time.Second)},
			wantLen:    1,
			wantGrace:  3 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "isolate policy",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithPanicPolicy(PanicIsolate)},
			wantLen:    1,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicIsolate,
		},
		{
			name:       "explicit fail policy",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithPanicPolicy(PanicFail)},
			wantLen:    1,
			wantGrace:  10 * time.Second,
			wantPolicy: PanicFail,
		},
		{
			name:       "later option wins",
			runners:    []Runner{&fakeRunner{"a"}},
			opts:       []Option{WithGrace(time.Second), WithGrace(2 * time.Second)},
			wantLen:    1,
			wantGrace:  2 * time.Second,
			wantPolicy: PanicFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.runners, tt.opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if len(s.runners) != tt.wantLen {
				t.Errorf("len(runners) = %d, want %d", len(s.runners), tt.wantLen)
			}
			if s.grace != tt.wantGrace {
				t.Errorf("grace = %v, want %v", s.grace, tt.wantGrace)
			}
			if s.policy != tt.wantPolicy {
				t.Errorf("policy = %v, want %v", s.policy, tt.wantPolicy)
			}
			if s.ran.Load() {
				t.Errorf("a new Supervisor reports it already ran")
			}
		})
	}
}

func TestPanicFailIsTheZeroPolicy(t *testing.T) {
	var p PanicPolicy
	if p != PanicFail {
		t.Errorf("zero PanicPolicy = %d, want PanicFail", p)
	}
}

func TestNewCopiesTheRunnerSlice(t *testing.T) {
	a, b := &fakeRunner{"a"}, &fakeRunner{"b"}
	runners := []Runner{a, b}
	s, err := New(runners)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runners[0] = nil
	runners[1] = &fakeRunner{"z"}
	if s.runners[0] != Runner(a) || s.runners[1] != Runner(b) {
		t.Errorf("Supervisor runners changed with the caller's slice")
	}
}

func TestDefaultStopContextIsBoundedByGrace(t *testing.T) {
	const grace = time.Minute
	s, err := New([]Runner{&fakeRunner{"a"}}, WithGrace(grace))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := s.newStopContext()
	defer cancel()

	if err := ctx.Err(); err != nil {
		t.Fatalf("stop context already done: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("stop context has no deadline")
	}
	if time.Until(deadline) > grace {
		t.Errorf("deadline is %v away, want at most %v", time.Until(deadline), grace)
	}
}

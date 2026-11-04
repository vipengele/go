package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func allStates() []struct {
	state State
	text  string
} {
	return []struct {
		state State
		text  string
	}{
		{StateUnknown, "unknown"},
		{StateNotInstalled, "not_installed"},
		{StateStopped, "stopped"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateFailed, "failed"},
	}
}

func TestStateTextRoundTripsEveryState(t *testing.T) {
	for _, tc := range allStates() {
		if got := tc.state.String(); got != tc.text {
			t.Errorf("String(%d) = %q, want %q", int(tc.state), got, tc.text)
		}
		b, err := tc.state.MarshalText()
		if err != nil || string(b) != tc.text {
			t.Errorf("MarshalText(%d) = %q, %v, want %q", int(tc.state), b, err, tc.text)
		}
		back := StateFailed
		if err := back.UnmarshalText(b); err != nil || back != tc.state {
			t.Errorf("UnmarshalText(%q) = %v, %v, want %v", b, back, err, tc.state)
		}
	}
}

func TestStateUnmarshalTextRejectsUnknownNames(t *testing.T) {
	for _, in := range []string{"", "RUNNING", "bogus", " running", "not-installed"} {
		s := StateStopped
		if err := s.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("UnmarshalText(%q) succeeded, want error", in)
		}
		if s != StateStopped {
			t.Errorf("UnmarshalText(%q) changed state to %v", in, s)
		}
	}
}

func TestStateOutOfRangeValue(t *testing.T) {
	for _, s := range []State{State(7), State(-1)} {
		if got := s.String(); !strings.HasPrefix(got, "State(") || !strings.HasSuffix(got, ")") {
			t.Errorf("String(%d) = %q, want State(n)", int(s), got)
		}
		if _, err := s.MarshalText(); err == nil {
			t.Errorf("MarshalText(%d) succeeded, want error", int(s))
		}
	}
	if got := State(7).String(); got != "State(7)" {
		t.Errorf("String(7) = %q, want State(7)", got)
	}
}

func TestStatusJSONUsesLowerCaseKeysAndTextState(t *testing.T) {
	in := Status{State: StateRunning, Enabled: true, PID: 42, Restarts: 2, Detail: "ok"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"state":"running","enabled":true,"pid":42,"restarts":2,"detail":"ok"}`
	if string(b) != want {
		t.Fatalf("Marshal = %s, want %s", b, want)
	}
	var out Status
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestResolveTurnsARestartedStartIntoAFailure(t *testing.T) {
	tests := []struct {
		name     string
		observed State
		restarts int
		want     State
	}{
		{"starting without restarts stays starting", StateStarting, 0, StateStarting},
		{"starting with restarts fails", StateStarting, 3, StateFailed},
		{"running with restarts stays running", StateRunning, 3, StateRunning},
		{"failed without restarts stays failed", StateFailed, 0, StateFailed},
	}
	for _, tc := range allStates() {
		if tc.state == StateStarting {
			continue
		}
		for _, r := range []int{0, 5} {
			tests = append(tests, struct {
				name     string
				observed State
				restarts int
				want     State
			}{tc.text + " is unchanged", tc.state, r, tc.state})
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.observed, tc.restarts); got != tc.want {
				t.Errorf("resolve(%v, %d) = %v, want %v", tc.observed, tc.restarts, got, tc.want)
			}
		})
	}
}

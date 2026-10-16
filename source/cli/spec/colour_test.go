package spec_test

import (
	"testing"

	"go.vipengele.org/cli/spec"
)

// terminalAnswer is what the injected Env.Terminal does when ColorAllowed queries it.
type terminalAnswer int

const (
	// terminalUnreachable fails the test: a rule above the terminal row decides.
	terminalUnreachable terminalAnswer = iota
	// terminalYes reports a terminal.
	terminalYes
	// terminalNo reports no terminal.
	terminalNo
)

func colourEnv(t *testing.T, vars map[string]string, answer terminalAnswer) *spec.Env {
	t.Helper()
	return &spec.Env{
		LookupEnv: func(key string) (string, bool) {
			v, ok := vars[key]
			return v, ok
		},
		Terminal: func(s spec.Stream) bool {
			switch answer {
			case terminalYes:
				return true
			case terminalNo:
				return false
			default:
				t.Fatalf("Env.Terminal(%s) called although a rule above the terminal row decides", s)
				return false
			}
		},
	}
}

func colourIdentity() *spec.Identity {
	return &spec.Identity{Name: "Tool", Owner: "owner", Repo: "repo", EnvPrefix: "ATK", DirName: "tool"}
}

func TestColorAllowedPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		noColor  bool
		identity *spec.Identity
		vars     map[string]string
		terminal terminalAnswer
		want     bool
	}{
		{"flag turns colour off on a terminal", true, colourIdentity(), nil, terminalUnreachable, false},
		{"flag wins over FORCE_COLOR", true, colourIdentity(), map[string]string{"FORCE_COLOR": "1"}, terminalUnreachable, false},
		{"NO_COLOR turns colour off on a terminal", false, colourIdentity(), map[string]string{"NO_COLOR": "1"}, terminalUnreachable, false},
		{"prefixed NO_COLOR turns colour off on a terminal", false, colourIdentity(), map[string]string{"ATK_NO_COLOR": "1"}, terminalUnreachable, false},
		{"NO_COLOR wins over FORCE_COLOR", false, colourIdentity(), map[string]string{"NO_COLOR": "1", "FORCE_COLOR": "1"}, terminalUnreachable, false},
		{"prefixed NO_COLOR wins over FORCE_COLOR", false, colourIdentity(), map[string]string{"ATK_NO_COLOR": "1", "FORCE_COLOR": "1"}, terminalUnreachable, false},
		{"empty NO_COLOR counts as unset", false, colourIdentity(), map[string]string{"NO_COLOR": ""}, terminalYes, true},
		{"empty prefixed NO_COLOR counts as unset", false, colourIdentity(), map[string]string{"ATK_NO_COLOR": ""}, terminalYes, true},
		{"FORCE_COLOR turns colour on for a non-terminal", false, colourIdentity(), map[string]string{"FORCE_COLOR": "1"}, terminalUnreachable, true},
		{"FORCE_COLOR wins over TERM=dumb", false, colourIdentity(), map[string]string{"FORCE_COLOR": "1", "TERM": "dumb"}, terminalUnreachable, true},
		{"FORCE_COLOR=0 does not force colour on a non-terminal", false, colourIdentity(), map[string]string{"FORCE_COLOR": "0"}, terminalNo, false},
		{"FORCE_COLOR=0 leaves TERM=dumb to decide", false, colourIdentity(), map[string]string{"FORCE_COLOR": "0", "TERM": "dumb"}, terminalUnreachable, false},
		{"empty FORCE_COLOR counts as unset", false, colourIdentity(), map[string]string{"FORCE_COLOR": ""}, terminalNo, false},
		{"TERM=dumb turns colour off on a terminal", false, colourIdentity(), map[string]string{"TERM": "dumb"}, terminalUnreachable, false},
		{"empty TERM leaves the terminal to decide", false, colourIdentity(), map[string]string{"TERM": ""}, terminalYes, true},
		{"another TERM leaves the terminal to decide", false, colourIdentity(), map[string]string{"TERM": "xterm-256color"}, terminalYes, true},
		{"terminal turns colour on", false, colourIdentity(), nil, terminalYes, true},
		{"non-terminal turns colour off", false, colourIdentity(), nil, terminalNo, false},
		{"nil identity reads no prefixed NO_COLOR", false, nil, map[string]string{"ATK_NO_COLOR": "1"}, terminalYes, true},
		{"nil identity still reads NO_COLOR", false, nil, map[string]string{"NO_COLOR": "1"}, terminalUnreachable, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := colourEnv(t, tt.vars, tt.terminal)
			if got := spec.ColorAllowed(tt.noColor, tt.identity, env, spec.Stdout); got != tt.want {
				t.Errorf("ColorAllowed() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestColorAllowedQueriesTheGivenStream(t *testing.T) {
	for _, stream := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
		t.Run(stream.String(), func(t *testing.T) {
			env := &spec.Env{Terminal: func(s spec.Stream) bool { return s == stream }}
			if !spec.ColorAllowed(false, nil, env, stream) {
				t.Errorf("ColorAllowed(%s) = false, want true for the stream the Terminal reports", stream)
			}
		})
	}
}

func TestColorAllowedWithNilLookupEnvSetsNoVariable(t *testing.T) {
	tests := []struct {
		name     string
		terminal bool
	}{
		{"terminal turns colour on", true},
		{"non-terminal turns colour off", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &spec.Env{Terminal: func(spec.Stream) bool { return tt.terminal }}
			if got := spec.ColorAllowed(false, colourIdentity(), env, spec.Stdout); got != tt.terminal {
				t.Errorf("ColorAllowed() = %t, want %t", got, tt.terminal)
			}
		})
	}
}

func TestColorAllowedWithNilEnvIsOff(t *testing.T) {
	if spec.ColorAllowed(false, colourIdentity(), nil, spec.Stdout) {
		t.Error("ColorAllowed() with a nil Env = true, want false")
	}
}

func TestColorAllowedWithAnInvalidPrefixReadsNoPrefixedVariable(t *testing.T) {
	id := colourIdentity()
	id.EnvPrefix = ""
	env := colourEnv(t, map[string]string{"_NO_COLOR": "1"}, terminalYes)
	if !spec.ColorAllowed(false, id, env, spec.Stdout) {
		t.Error("ColorAllowed() = false, want true when EnvPrefix names no variable")
	}
}

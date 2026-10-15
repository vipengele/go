package cli

import (
	"bytes"
	"context"
	"os"
	"slices"
	"testing"

	"go.vipengele.org/cli/spec"
)

// terminalProbe returns a root whose run function records, for each standard stream, what the
// Env it receives answers to IsTerminal, and whether that Env carries a Terminal.
func terminalProbe(seen *[3]bool, hasTerminal *bool) *spec.Command {
	return &spec.Command{
		Name: "tool",
		Run: func(_ context.Context, inv *spec.Invocation) error {
			*hasTerminal = inv.Env.Terminal != nil
			for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
				seen[s] = inv.Env.IsTerminal(s)
			}
			return nil
		},
	}
}

func TestExecuteArgsDetectsTerminalOfRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	env := &spec.Env{Stdin: f, Stdout: f, Stderr: f}
	var seen [3]bool
	var hasTerminal bool
	if code := ExecuteArgs(env, terminalProbe(&seen, &hasTerminal), nil); code != ExitOK {
		t.Fatalf("code %d, want %d", code, ExitOK)
	}
	if !hasTerminal {
		t.Error("run function's Env has a nil Terminal")
	}
	if seen != [3]bool{} {
		t.Errorf("IsTerminal of a regular file %v, want all false", seen)
	}
	if env.Terminal != nil {
		t.Error("caller's Env.Terminal set, want nil")
	}
}

func TestExecuteArgsDetectsTerminalOfBuffers(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := &spec.Env{Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr}
	var seen [3]bool
	var hasTerminal bool
	if code := ExecuteArgs(env, terminalProbe(&seen, &hasTerminal), nil); code != ExitOK {
		t.Fatalf("code %d, want %d", code, ExitOK)
	}
	if !hasTerminal {
		t.Error("run function's Env has a nil Terminal")
	}
	if seen != [3]bool{} {
		t.Errorf("IsTerminal of buffers %v, want all false", seen)
	}
	if env.Terminal != nil {
		t.Error("caller's Env.Terminal set, want nil")
	}
}

func TestExecuteArgsDetectsTerminalOfNilEnv(t *testing.T) {
	var seen [3]bool
	var hasTerminal bool
	if code := ExecuteArgs(nil, terminalProbe(&seen, &hasTerminal), nil); code != ExitOK {
		t.Fatalf("code %d, want %d", code, ExitOK)
	}
	if !hasTerminal {
		t.Error("run function's Env has a nil Terminal")
	}
	if seen != [3]bool{} {
		t.Errorf("IsTerminal of the default streams %v, want all false", seen)
	}
}

func TestExecuteArgsKeepsSuppliedTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	var asked []spec.Stream
	supplied := func(s spec.Stream) bool {
		asked = append(asked, s)
		return s == spec.Stdout
	}
	env := &spec.Env{Stdin: f, Stdout: f, Stderr: f, Terminal: supplied}
	var seen [3]bool
	var hasTerminal bool
	if code := ExecuteArgs(env, terminalProbe(&seen, &hasTerminal), nil); code != ExitOK {
		t.Fatalf("code %d, want %d", code, ExitOK)
	}
	if want := [3]bool{false, true, false}; seen != want {
		t.Errorf("IsTerminal %v, want %v as the supplied Terminal answers", seen, want)
	}
	for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
		if !slices.Contains(asked, s) {
			t.Errorf("supplied Terminal not asked about %v; asked %v", s, asked)
		}
	}
}

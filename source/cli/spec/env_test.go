package spec_test

import (
	"io"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

type recordingWriter struct {
	strings.Builder
}

func assertInertEnv(t *testing.T, env *spec.Env) {
	t.Helper()
	if env.Stdin == nil || env.Stdout == nil || env.Stderr == nil || env.LookupEnv == nil {
		t.Fatalf("resolved Env has a nil field: %+v", env)
	}
	data, err := io.ReadAll(env.Stdin)
	if err != nil || len(data) != 0 {
		t.Errorf("Stdin read %q, %v, want empty, nil", data, err)
	}
	if env.Stdout != io.Discard || env.Stderr != io.Discard {
		t.Error("Stdout and Stderr are not io.Discard")
	}
	if v, ok := env.LookupEnv("HOME"); ok || v != "" {
		t.Errorf("LookupEnv(HOME) = %q, %t, want empty, false", v, ok)
	}
	if env.WorkDir != "" {
		t.Errorf("WorkDir = %q, want empty", env.WorkDir)
	}
}

func TestEnvResolvedOfNil(t *testing.T) {
	var env *spec.Env
	assertInertEnv(t, env.Resolved())
}

func TestEnvResolvedOfZero(t *testing.T) {
	assertInertEnv(t, (&spec.Env{}).Resolved())
}

func TestEnvResolvedKeepsSetFields(t *testing.T) {
	var out, errOut recordingWriter
	in := strings.NewReader("input")
	lookup := func(key string) (string, bool) { return "v-" + key, true }
	env := &spec.Env{Stdin: in, Stdout: &out, Stderr: &errOut, WorkDir: "/work", LookupEnv: lookup}

	r := env.Resolved()
	if r == env {
		t.Fatal("Resolved returned its receiver, want a copy")
	}
	if r.Stdin != in || r.Stdout != &out || r.Stderr != &errOut || r.WorkDir != "/work" {
		t.Errorf("Resolved changed a set field: %+v", r)
	}
	if v, ok := r.LookupEnv("X"); !ok || v != "v-X" {
		t.Errorf("LookupEnv(X) = %q, %t, want v-X, true", v, ok)
	}
}

func TestEnvResolvedFreshStdinPerCall(t *testing.T) {
	var env *spec.Env
	first := env.Resolved()
	second := env.Resolved()
	if _, err := io.ReadAll(first.Stdin); err != nil {
		t.Fatal(err)
	}
	if first.Stdin == second.Stdin {
		t.Error("two resolved Envs share one default Stdin")
	}
}

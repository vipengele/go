package spec_test

import (
	"slices"
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestNewInvocationResolvesANilEnv(t *testing.T) {
	inv := spec.NewInvocation(nil, []string{"a"}, nil)
	if inv.Env == nil {
		t.Fatal("Env is nil")
	}
	assertInertEnv(t, inv.Env)
	if !slices.Equal(inv.Args, []string{"a"}) {
		t.Errorf("Args = %q, want [a]", inv.Args)
	}
}

func TestNewInvocationKeepsGivenEnvFields(t *testing.T) {
	var out, errOut recordingWriter
	env := &spec.Env{Stdout: &out, Stderr: &errOut, WorkDir: "/w"}
	inv := spec.NewInvocation(env, nil, nil)
	if inv.Env.Stdout != &out || inv.Env.Stderr != &errOut {
		t.Error("Stdout or Stderr was replaced")
	}
	if inv.Env.WorkDir != "/w" {
		t.Errorf("WorkDir = %q, want /w", inv.Env.WorkDir)
	}
	if inv.Env.Stdin == nil || inv.Env.LookupEnv == nil {
		t.Error("nil Stdin or LookupEnv was not given a default")
	}
	if env.Stdin != nil || env.LookupEnv != nil {
		t.Error("NewInvocation modified the caller's Env")
	}
}

func TestNewInvocationCopiesArgsAndValues(t *testing.T) {
	name := spec.StringFlag("name", "")
	args := []string{"a", "b"}
	values := map[string]any{"name": "first"}
	inv := spec.NewInvocation(nil, args, values)

	args[0] = "changed"
	values["name"] = "changed"
	if inv.Args[0] != "a" {
		t.Errorf("Args[0] = %q after the caller changed its slice, want %q", inv.Args[0], "a")
	}
	if got := name.Get(inv); got != "first" {
		t.Errorf("Get = %q after the caller changed its map, want %q", got, "first")
	}
}

func TestNewInvocationLeavesTheDefaultOutput(t *testing.T) {
	inv := spec.NewInvocation(nil, nil, nil)
	if inv.Output != (spec.Output{}) {
		t.Errorf("Output = %+v, want the zero Output", inv.Output)
	}
}

func TestNewInvocationCopiesTheExplicitFlags(t *testing.T) {
	asJSON := spec.BoolFlag("json", "")
	names := []string{"json"}
	inv := spec.NewInvocation(nil, nil, map[string]any{"json": false, "other": true},
		spec.ExplicitFlags(names...))

	names[0] = "other"
	if !asJSON.IsSet(inv) {
		t.Error("IsSet = false after the caller changed its slice, want true")
	}
	if other := spec.BoolFlag("other", ""); other.IsSet(inv) {
		t.Error("IsSet of a flag the option does not name = true, want false")
	}
}

package config_test

import (
	"context"
	"testing"

	"go.vipengele.org/cli/core/command/config"
	cfg "go.vipengele.org/config"
)

// other is a configuration type distinct from testConfig.
type other struct {
	Name string
}

// mustLoadOther returns a loaded configuration of type other.
func mustLoadOther(t *testing.T) cfg.Result[other] {
	t.Helper()
	res, err := cfg.Load(other{Name: "x"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

func TestWithFromRoundTrip(t *testing.T) {
	want, err := cfg.Load(testConfig{Name: "tool", Port: 80})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := config.From[testConfig](config.With(context.Background(), want))
	if !ok {
		t.Fatal("From reports no configuration, want the one stored")
	}
	if got.Value() != want.Value() {
		t.Errorf("Value = %+v, want %+v", got.Value(), want.Value())
	}
	if len(got.Sources()) != len(want.Sources()) {
		t.Errorf("Sources = %v, want %v", got.Sources(), want.Sources())
	}
}

func TestFromReportsFalseOnABareContext(t *testing.T) {
	if _, ok := config.From[testConfig](context.Background()); ok {
		t.Error("From on a bare context reports a configuration, want none")
	}
}

func TestFromReportsFalseForAnotherType(t *testing.T) {
	ctx := config.With(context.Background(), mustLoadOther(t))
	if _, ok := config.From[testConfig](ctx); ok {
		t.Error("From[testConfig] on a context carrying another type reports a configuration, want none")
	}
	if _, ok := config.From[other](ctx); !ok {
		t.Error("From[other] reports no configuration, want the one stored")
	}
}

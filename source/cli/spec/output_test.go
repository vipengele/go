package spec_test

import (
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestOutputAccessors(t *testing.T) {
	tests := []struct {
		name                         string
		out                          spec.Output
		verbose, quiet, json, colour bool
	}{
		{"zero value is the default presentation", spec.Output{}, false, false, false, true},
		{"zero options build the zero value", spec.NewOutput(spec.OutputOptions{}), false, false, false, true},
		{"verbose", spec.NewOutput(spec.OutputOptions{Verbose: true}), true, false, false, true},
		{"quiet", spec.NewOutput(spec.OutputOptions{Quiet: true}), false, true, false, true},
		{"json", spec.NewOutput(spec.OutputOptions{JSON: true}), false, false, true, true},
		{"no colour", spec.NewOutput(spec.OutputOptions{NoColor: true}), false, false, false, false},
		{
			"conflicting settings are kept as given",
			spec.NewOutput(spec.OutputOptions{Verbose: true, Quiet: true, JSON: true, NoColor: true}),
			true, true, true, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.out.Verbose(); got != tt.verbose {
				t.Errorf("Verbose() = %t, want %t", got, tt.verbose)
			}
			if got := tt.out.Quiet(); got != tt.quiet {
				t.Errorf("Quiet() = %t, want %t", got, tt.quiet)
			}
			if got := tt.out.JSON(); got != tt.json {
				t.Errorf("JSON() = %t, want %t", got, tt.json)
			}
			if got := tt.out.Color(); got != tt.colour {
				t.Errorf("Color() = %t, want %t", got, tt.colour)
			}
		})
	}
}

func TestZeroOutputEqualsNewOutputOfZeroOptions(t *testing.T) {
	if spec.NewOutput(spec.OutputOptions{}) != (spec.Output{}) {
		t.Error("NewOutput(OutputOptions{}) differs from the zero Output")
	}
}

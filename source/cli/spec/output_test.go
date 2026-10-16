package spec_test

import (
	"testing"

	"go.vipengele.org/cli/spec"
)

func TestOutputAccessors(t *testing.T) {
	tests := []struct {
		name                 string
		out                  spec.Output
		verbose, quiet, json bool
		stdout, stderr       bool
	}{
		{"zero value is the default presentation", spec.Output{}, false, false, false, true, true},
		{"zero options build the zero value", spec.NewOutput(spec.OutputOptions{}), false, false, false, true, true},
		{"verbose", spec.NewOutput(spec.OutputOptions{Verbose: true}), true, false, false, true, true},
		{"quiet", spec.NewOutput(spec.OutputOptions{Quiet: true}), false, true, false, true, true},
		{"json", spec.NewOutput(spec.OutputOptions{JSON: true}), false, false, true, true, true},
		{
			"no colour on stdout keeps colour on stderr",
			spec.NewOutput(spec.OutputOptions{NoColorStdout: true}),
			false, false, false, false, true,
		},
		{
			"no colour on stderr keeps colour on stdout",
			spec.NewOutput(spec.OutputOptions{NoColorStderr: true}),
			false, false, false, true, false,
		},
		{
			"no colour on either stream",
			spec.NewOutput(spec.OutputOptions{NoColorStdout: true, NoColorStderr: true}),
			false, false, false, false, false,
		},
		{
			"conflicting settings are kept as given",
			spec.NewOutput(spec.OutputOptions{Verbose: true, Quiet: true, JSON: true, NoColorStdout: true}),
			true, true, true, false, true,
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
			if got := tt.out.Color(spec.Stdout); got != tt.stdout {
				t.Errorf("Color(Stdout) = %t, want %t", got, tt.stdout)
			}
			if got := tt.out.Color(spec.Stderr); got != tt.stderr {
				t.Errorf("Color(Stderr) = %t, want %t", got, tt.stderr)
			}
		})
	}
}

func TestOutputColorIsFalseForStreamsThatTakeNoOutput(t *testing.T) {
	for _, s := range []spec.Stream{spec.Stdin, spec.Stream(-1), spec.Stream(3)} {
		if spec.NewOutput(spec.OutputOptions{}).Color(s) {
			t.Errorf("Color(%v) = true on the zero Output, want false", s)
		}
	}
}

func TestZeroOutputEqualsNewOutputOfZeroOptions(t *testing.T) {
	if spec.NewOutput(spec.OutputOptions{}) != (spec.Output{}) {
		t.Error("NewOutput(OutputOptions{}) differs from the zero Output")
	}
}

func TestOutputFormat(t *testing.T) {
	tests := []struct {
		name string
		out  spec.Output
		want spec.Format
	}{
		{"zero value is text", spec.Output{}, spec.FormatText},
		{"zero options are text", spec.NewOutput(spec.OutputOptions{}), spec.FormatText},
		{"explicit text", spec.NewOutput(spec.OutputOptions{Format: spec.FormatText}), spec.FormatText},
		{"explicit json", spec.NewOutput(spec.OutputOptions{Format: spec.FormatJSON}), spec.FormatJSON},
		{"json option selects json", spec.NewOutput(spec.OutputOptions{JSON: true}), spec.FormatJSON},
		{
			"json option wins over explicit text",
			spec.NewOutput(spec.OutputOptions{Format: spec.FormatText, JSON: true}),
			spec.FormatJSON,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.out.Format(); got != tt.want {
				t.Errorf("Format() = %v, want %v", got, tt.want)
			}
			if got, want := tt.out.JSON(), tt.want == spec.FormatJSON; got != want {
				t.Errorf("JSON() = %t, want %t", got, want)
			}
		})
	}
}

func TestFormatString(t *testing.T) {
	for f, want := range map[spec.Format]string{spec.FormatText: "text", spec.FormatJSON: "json", spec.Format(9): "unknown"} {
		if got := f.String(); got != want {
			t.Errorf("Format(%d).String() = %q, want %q", int(f), got, want)
		}
	}
}

package spec_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

// failingWriter fails every write with err.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestInvocationMessages(t *testing.T) {
	type helper struct {
		name string
		call func(*spec.Invocation)
		// line is what the helper writes with colour off, when it writes.
		line string
		// sgr is the SGR sequence that opens its line with colour on; "" for unstyled output.
		sgr    string
		stdout bool
		writes func(spec.OutputOptions) bool
	}
	always := func(spec.OutputOptions) bool { return true }
	unlessQuiet := func(o spec.OutputOptions) bool { return !o.Quiet }
	helpers := []helper{
		{"info", func(inv *spec.Invocation) { inv.Info("n=%d", 7) },
			spec.GlyphInfo + " n=7\n", "\x1b[34m", false, unlessQuiet},
		{"success", func(inv *spec.Invocation) { inv.Success("n=%d", 7) },
			spec.GlyphSuccess + " n=7\n", "\x1b[32m", false, unlessQuiet},
		{"warning", func(inv *spec.Invocation) { inv.Warning("n=%d", 7) },
			spec.GlyphWarning + " n=7\n", "\x1b[33m", false, unlessQuiet},
		{"error", func(inv *spec.Invocation) { inv.Error("n=%d", 7) },
			spec.GlyphError + " n=7\n", "\x1b[31m", false, always},
		{"detail", func(inv *spec.Invocation) { inv.Detail("n=%d", 7) },
			"n=7\n", "\x1b[2m", false, func(o spec.OutputOptions) bool { return o.Verbose && !o.Quiet }},
		{"data", func(inv *spec.Invocation) {
			if err := inv.Data("n=%d", 7); err != nil {
				t.Errorf("Data returned %v, want nil", err)
			}
		}, "n=7", "", true, always},
		{"info with a dynamic string", func(inv *spec.Invocation) {
			msg := "100% done"
			inv.Info("%s", msg)
		}, spec.GlyphInfo + " 100% done\n", "\x1b[34m", false, unlessQuiet},
	}
	modes := []struct {
		name string
		opts spec.OutputOptions
	}{
		{"default", spec.OutputOptions{}},
		{"quiet", spec.OutputOptions{Quiet: true}},
		{"verbose", spec.OutputOptions{Verbose: true}},
		{"json", spec.OutputOptions{JSON: true}},
	}
	type row struct {
		name       string
		helper     helper
		opts       spec.OutputOptions
		color      bool
		wantStdout string
		wantStderr string
	}
	var rows []row
	for _, h := range helpers {
		for _, m := range modes {
			for _, color := range []bool{true, false} {
				opts := m.opts
				opts.NoColorStdout, opts.NoColorStderr = !color, !color
				r := row{name: h.name + "/" + m.name, helper: h, opts: opts, color: color}
				if color {
					r.name += "/colour"
				} else {
					r.name += "/no-colour"
				}
				if h.writes(m.opts) {
					if h.stdout {
						r.wantStdout = h.line
					} else {
						r.wantStderr = h.line
					}
				}
				rows = append(rows, r)
			}
		}
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			inv := &spec.Invocation{
				Env:    &spec.Env{Stdout: &stdout, Stderr: &stderr},
				Output: spec.NewOutput(tc.opts),
			}
			tc.helper.call(inv)

			gotOut, gotErr := stdout.String(), stderr.String()
			if !tc.color {
				if gotOut != tc.wantStdout {
					t.Errorf("stdout = %q, want %q", gotOut, tc.wantStdout)
				}
				if gotErr != tc.wantStderr {
					t.Errorf("stderr = %q, want %q", gotErr, tc.wantStderr)
				}
				return
			}
			if got := stripSGR(gotOut); got != tc.wantStdout {
				t.Errorf("stdout without SGR = %q, want %q", got, tc.wantStdout)
			}
			if got := stripSGR(gotErr); got != tc.wantStderr {
				t.Errorf("stderr without SGR = %q, want %q", got, tc.wantStderr)
			}
			if tc.helper.stdout && gotOut != tc.wantStdout {
				t.Errorf("stdout = %q, want %q with no styling", gotOut, tc.wantStdout)
			}
			if tc.wantStderr != "" && !strings.HasPrefix(gotErr, tc.helper.sgr) {
				t.Errorf("stderr = %q, want it to open with %q", gotErr, tc.helper.sgr)
			}
		})
	}
}

func TestInvocationMessagesWithoutEnvDiscard(t *testing.T) {
	inv := &spec.Invocation{}
	inv.Info("info %d", 1)
	inv.Success("success %d", 2)
	inv.Warning("warning %d", 3)
	inv.Error("error %d", 4)
	inv.Detail("detail %d", 5)
	if err := inv.Data("data %d", 6); err != nil {
		t.Errorf("Data on a zero Invocation returned %v, want nil", err)
	}
}

func TestInvocationDataReturnsWriteError(t *testing.T) {
	want := errors.New("disk full")
	inv := &spec.Invocation{Env: &spec.Env{Stdout: failingWriter{want}}}
	if err := inv.Data("x"); !errors.Is(err, want) {
		t.Errorf("Data returned %v, want %v", err, want)
	}
}

func TestInvocationMessagesIgnoreWriteError(t *testing.T) {
	inv := &spec.Invocation{
		Env:    &spec.Env{Stderr: failingWriter{errors.New("closed")}},
		Output: spec.NewOutput(spec.OutputOptions{Verbose: true}),
	}
	inv.Info("info")
	inv.Success("success")
	inv.Warning("warning")
	inv.Error("error")
	inv.Detail("detail")
}

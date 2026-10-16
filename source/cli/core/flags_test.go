package cli_test

import (
	"context"
	"strings"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

func allStandardFlags() []cli.Option {
	return []cli.Option{cli.WithVerbose(), cli.WithQuiet(), cli.WithNoColor(), cli.WithJSON()}
}

func TestAppStandardFlags(t *testing.T) {
	tests := []struct {
		name        string
		opts        []cli.Option
		args        []string
		wantVerbose bool
		wantQuiet   bool
		wantJSON    bool
	}{
		{name: "none set", opts: allStandardFlags()},
		{name: "--verbose", opts: allStandardFlags(), args: []string{"--verbose"}, wantVerbose: true},
		{name: "-v", opts: allStandardFlags(), args: []string{"-v"}, wantVerbose: true},
		{name: "--quiet", opts: allStandardFlags(), args: []string{"--quiet"}, wantQuiet: true},
		{name: "-q", opts: allStandardFlags(), args: []string{"-q"}, wantQuiet: true},
		{name: "--json", opts: allStandardFlags(), args: []string{"--json"}, wantJSON: true},
		{name: "-v --json", opts: allStandardFlags(), args: []string{"-v", "--json"}, wantVerbose: true, wantJSON: true},
		{name: "one flag opted in", opts: []cli.Option{cli.WithJSON()}, args: []string{"--json"}, wantJSON: true},
		{name: "no option", args: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got spec.Output
			app := outputApp(&got, tt.opts...)
			args := append([]string{"show"}, tt.args...)
			r := runApp(t, app, args...)
			if r.code != cli.ExitOK {
				t.Fatalf("ExecuteArgs(%q) = %d, stderr %q; want 0", args, r.code, r.stderr)
			}
			if got.Verbose() != tt.wantVerbose || got.Quiet() != tt.wantQuiet || got.JSON() != tt.wantJSON {
				t.Errorf("Output verbose=%t quiet=%t json=%t, want verbose=%t quiet=%t json=%t",
					got.Verbose(), got.Quiet(), got.JSON(), tt.wantVerbose, tt.wantQuiet, tt.wantJSON)
			}
		})
	}
}

func TestAppStandardFlagsReachNestedCommands(t *testing.T) {
	var got spec.Output
	app := cli.New(identity(), allStandardFlags()...)
	app.Add(&spec.Command{
		Name: "remote",
		Commands: []*spec.Command{{
			Name: "add",
			Run: func(_ context.Context, inv *spec.Invocation) error {
				got = inv.Output
				return nil
			},
		}},
	})
	r := runApp(t, app, "remote", "add", "-q", "--json")
	if r.code != cli.ExitOK {
		t.Fatalf("code %d, stderr %q; want 0", r.code, r.stderr)
	}
	if !got.Quiet() || !got.JSON() {
		t.Errorf("Output quiet=%t json=%t, want both true", got.Quiet(), got.JSON())
	}
}

func TestAppStandardFlagErrors(t *testing.T) {
	tests := []struct {
		name string
		opts []cli.Option
		args []string
		want result
	}{
		{
			name: "--verbose with --quiet",
			opts: allStandardFlags(),
			args: []string{"show", "--verbose", "--quiet"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: show: --verbose and --quiet cannot be used together\n"},
		},
		{
			name: "-v with -q",
			opts: allStandardFlags(),
			args: []string{"show", "-vq"},
			want: result{code: cli.ExitUsage,
				stderr: "tool: show: --verbose and --quiet cannot be used together\n"},
		},
		{
			name: "--json not opted in",
			opts: []cli.Option{cli.WithVerbose()},
			args: []string{"show", "--json"},
			want: result{code: cli.ExitUsage, stderr: "tool: show: unknown flag: --json\n"},
		},
		{
			name: "-v not opted in",
			args: []string{"show", "-v"},
			want: result{code: cli.ExitUsage, stderr: "tool: show: unknown shorthand flag: 'v' in -v\n"},
		},
		{
			name: "--no-color not opted in",
			args: []string{"show", "--no-color"},
			want: result{code: cli.ExitUsage, stderr: "tool: show: unknown flag: --no-color\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got spec.Output
			if r := runApp(t, outputApp(&got, tt.opts...), tt.args...); r != tt.want {
				t.Errorf("ExecuteArgs(%q)\n got: %+v\nwant: %+v", tt.args, r, tt.want)
			}
		})
	}
}

func TestAppStandardFlagCollision(t *testing.T) {
	ok := func(context.Context, *spec.Invocation) error { return nil }
	tests := []struct {
		name string
		opt  cli.Option
		flag spec.Flag
	}{
		{name: "--json", opt: cli.WithJSON(), flag: spec.BoolFlag("json", "write JSON")},
		{name: "--verbose", opt: cli.WithVerbose(), flag: spec.BoolFlag("verbose", "say more")},
		{name: "-v", opt: cli.WithVerbose(), flag: spec.StringFlag("vendor", "the vendor", spec.Shorthand("v"))},
		{name: "-q", opt: cli.WithQuiet(), flag: spec.BoolFlag("quick", "be quick", spec.Shorthand("q"))},
		{name: "--no-color", opt: cli.WithNoColor(), flag: spec.BoolFlag("no-color", "no colour")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := cli.New(identity(), tt.opt)
			app.Add(&spec.Command{Name: "show", Flags: []spec.Flag{tt.flag}, Run: ok})
			r := runApp(t, app, "show")
			if r.code != cli.ExitError || !strings.HasPrefix(r.stderr, "tool: invalid command tree\n") {
				t.Errorf("code %d, stderr %q; want 1 and an invalid command tree", r.code, r.stderr)
			}
		})
	}
}

func TestAppStandardFlagsWithoutOptionAllowTheCommandsOwn(t *testing.T) {
	var asJSON bool
	jsonFlag := spec.BoolFlag("json", "write JSON")
	app := cli.New(identity(), cli.WithVerbose())
	app.Add(&spec.Command{
		Name:  "show",
		Flags: []spec.Flag{jsonFlag},
		Run: func(_ context.Context, inv *spec.Invocation) error {
			asJSON = jsonFlag.Get(inv)
			if inv.Output.JSON() {
				t.Error("Output.JSON() is true for a flag the App does not take")
			}
			return nil
		},
	})
	if r := runApp(t, app, "show", "--json"); r.code != cli.ExitOK || !asJSON {
		t.Errorf("code %d, stderr %q, --json %t; want 0 and true", r.code, r.stderr, asJSON)
	}
}

func TestAppStandardFlagsHelp(t *testing.T) {
	tests := []struct {
		name    string
		opts    []cli.Option
		args    []string
		want    []string
		notWant []string
	}{
		{
			name: "root lists every opted-in flag",
			opts: allStandardFlags(),
			args: []string{"--help"},
			want: []string{"-v, --verbose", "-q, --quiet", "--no-color", "--json"},
		},
		{
			name: "command lists them as global flags",
			opts: allStandardFlags(),
			args: []string{"show", "--help"},
			want: []string{"Global Flags", "-v, --verbose", "-q, --quiet", "--no-color", "--json"},
		},
		{
			name:    "only the opted-in flag",
			opts:    []cli.Option{cli.WithQuiet()},
			args:    []string{"--help"},
			want:    []string{"-q, --quiet"},
			notWant: []string{"--verbose", "--no-color", "--json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got spec.Output
			r := runApp(t, outputApp(&got, tt.opts...), tt.args...)
			if r.code != cli.ExitOK {
				t.Fatalf("code %d, stderr %q; want 0", r.code, r.stderr)
			}
			for _, s := range tt.want {
				if !strings.Contains(r.stdout, s) {
					t.Errorf("help lacks %q:\n%s", s, r.stdout)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(r.stdout, s) {
					t.Errorf("help contains %q:\n%s", s, r.stdout)
				}
			}
		})
	}
}

// formatApp returns an App configured by opts whose command "show" stores the Output it
// receives in *got, and whose command "emit" declares a local --json and stores the same.
func formatApp(got *spec.Output, opts ...cli.Option) *cli.App {
	app := outputApp(got, opts...)
	app.Add(&spec.Command{
		Name:  "emit",
		Flags: []spec.Flag{spec.BoolFlag("json", "write JSON")},
		Run: func(_ context.Context, inv *spec.Invocation) error {
			*got = inv.Output
			return nil
		},
	})
	return app
}

func TestAppOutputFormat(t *testing.T) {
	output := []cli.Option{cli.WithOutput()}
	tests := []struct {
		name string
		opts []cli.Option
		args []string
		want spec.Format
	}{
		{name: "no format flag", args: []string{"show"}, want: spec.FormatText},
		{name: "--json not given", opts: []cli.Option{cli.WithJSON()}, args: []string{"show"}, want: spec.FormatText},
		{name: "--json", opts: []cli.Option{cli.WithJSON()}, args: []string{"show", "--json"}, want: spec.FormatJSON},
		{name: "--json=false", opts: []cli.Option{cli.WithJSON()}, args: []string{"show", "--json=false"}, want: spec.FormatText},
		{name: "--output not given", opts: output, args: []string{"show"}, want: spec.FormatText},
		{name: "--output text", opts: output, args: []string{"show", "--output", "text"}, want: spec.FormatText},
		{name: "--output json", opts: output, args: []string{"show", "--output", "json"}, want: spec.FormatJSON},
		{name: "--output=json before the command", opts: output, args: []string{"--output=json", "show"}, want: spec.FormatJSON},
		{name: "last --output wins", opts: output, args: []string{"show", "--output", "json", "--output", "text"}, want: spec.FormatText},
		{name: "last --output wins again", opts: output, args: []string{"show", "--output", "text", "--output", "json"}, want: spec.FormatJSON},
		{name: "a local --json alone is the command's own", opts: output, args: []string{"emit", "--json"}, want: spec.FormatText},
		{name: "a local --json with --output json", opts: output, args: []string{"emit", "--json", "--output", "json"}, want: spec.FormatJSON},
		{name: "a local --json=false with --output text", opts: output, args: []string{"emit", "--json=false", "--output", "text"}, want: spec.FormatText},
		{name: "last local --json wins", opts: output, args: []string{"emit", "--json", "--json=false", "--output", "text"}, want: spec.FormatText},
		{name: "--output json with a local --json not given", opts: output, args: []string{"emit", "--output", "json"}, want: spec.FormatJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got spec.Output
			app := outputApp(&got, tt.opts...)
			if tt.args[0] == "emit" {
				app = formatApp(&got, tt.opts...)
			}
			r := runApp(t, app, tt.args...)
			if r.code != cli.ExitOK {
				t.Fatalf("ExecuteArgs(%q) = %d, stderr %q; want 0", tt.args, r.code, r.stderr)
			}
			if got.Format() != tt.want || got.JSON() != (tt.want == spec.FormatJSON) {
				t.Errorf("Output format %v, JSON %t; want %v", got.Format(), got.JSON(), tt.want)
			}
		})
	}
}

func TestAppOutputFormatErrors(t *testing.T) {
	output := []cli.Option{cli.WithOutput()}
	tests := []struct {
		name string
		opts []cli.Option
		args []string
		want result
	}{
		{
			name: "an unknown --output",
			opts: output,
			args: []string{"show", "--output", "yaml"},
			want: result{code: cli.ExitUsage, stderr: "tool: show: --output must be text or json, not \"yaml\"\n"},
		},
		{
			name: "an empty --output",
			opts: output,
			args: []string{"show", "--output="},
			want: result{code: cli.ExitUsage, stderr: "tool: show: --output must be text or json, not \"\"\n"},
		},
		{
			name: "--output text with a local --json",
			opts: output,
			args: []string{"emit", "--output", "text", "--json"},
			want: result{code: cli.ExitUsage, stderr: "tool: emit: --output text and --json cannot be used together\n"},
		},
		{
			name: "--output json with a local --json=false",
			opts: output,
			args: []string{"emit", "--json=false", "--output", "json"},
			want: result{code: cli.ExitUsage, stderr: "tool: emit: --output json and --json=false cannot be used together\n"},
		},
		{
			name: "--json under --output",
			opts: output,
			args: []string{"show", "--json"},
			want: result{code: cli.ExitUsage, stderr: "tool: show: unknown flag: --json\n"},
		},
		{
			name: "--output not opted in",
			opts: []cli.Option{cli.WithJSON()},
			args: []string{"show", "--output", "json"},
			want: result{code: cli.ExitUsage, stderr: "tool: show: unknown flag: --output\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			app := cli.New(identity(), tt.opts...)
			run := func(context.Context, *spec.Invocation) error { ran = true; return nil }
			app.Add(&spec.Command{Name: "show", Run: run})
			if tt.args[0] == "emit" {
				app.Add(&spec.Command{Name: "emit", Flags: []spec.Flag{spec.BoolFlag("json", "write JSON")}, Run: run})
			}
			if r := runApp(t, app, tt.args...); r != tt.want {
				t.Errorf("ExecuteArgs(%q)\n got: %+v\nwant: %+v", tt.args, r, tt.want)
			}
			if ran {
				t.Error("the run function ran")
			}
		})
	}
}

func TestAppOutputFormatReachesNestedCommands(t *testing.T) {
	var got spec.Output
	app := cli.New(identity(), cli.WithVerbose(), cli.WithOutput())
	app.Add(&spec.Command{
		Name: "remote",
		Commands: []*spec.Command{{
			Name: "add",
			Run: func(_ context.Context, inv *spec.Invocation) error {
				got = inv.Output
				return nil
			},
		}},
	})
	r := runApp(t, app, "--output", "json", "remote", "add", "-v")
	if r.code != cli.ExitOK {
		t.Fatalf("code %d, stderr %q; want 0", r.code, r.stderr)
	}
	if got.Format() != spec.FormatJSON || !got.Verbose() {
		t.Errorf("Output format %v verbose %t, want json and true", got.Format(), got.Verbose())
	}
}

func TestNewPanicsForJSONWithOutput(t *testing.T) {
	for name, opts := range map[string][]cli.Option{
		"WithJSON first":   {cli.WithJSON(), cli.WithOutput()},
		"WithOutput first": {cli.WithOutput(), cli.WithVerbose(), cli.WithJSON()},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				msg, _ := recover().(string)
				if want := "WithJSON and WithOutput cannot be used together"; !strings.Contains(msg, want) {
					t.Errorf("panic %q, want a message containing %q", msg, want)
				}
			}()
			cli.New(identity(), opts...)
		})
	}
}

func TestAppOutputHelp(t *testing.T) {
	var got spec.Output
	for _, args := range [][]string{{"--help"}, {"show", "--help"}} {
		r := runApp(t, outputApp(&got, cli.WithOutput()), args...)
		if r.code != cli.ExitOK {
			t.Fatalf("%q: code %d, stderr %q; want 0", args, r.code, r.stderr)
		}
		for _, s := range []string{"--output string", "write the output as text or json", `(default "text")`} {
			if !strings.Contains(r.stdout, s) {
				t.Errorf("%q: help lacks %q:\n%s", args, s, r.stdout)
			}
		}
	}
}

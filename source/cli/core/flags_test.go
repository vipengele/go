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
			if r.code != cli.ExitError || !strings.HasPrefix(r.stderr, "tool: invalid command tree: ") {
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

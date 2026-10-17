package cli_test

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/logging"
	"go.vipengele.org/cli/spec"
)

// loggingApp returns an App with opts and a "run" command that records that it ran in ran.
func loggingApp(ran *bool, opts ...cli.Option) *cli.App {
	app := cli.New(identity(), opts...)
	app.Add(&spec.Command{Name: "run", Run: func(context.Context, *spec.Invocation) error {
		*ran = true
		return nil
	}})
	return app
}

func TestWithLoggingDeclaresInheritedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "root help", args: []string{"--help"}},
		{name: "subcommand help", args: []string{"run", "--help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ran bool
			app := loggingApp(&ran, cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging())
			got := runApp(t, app, tt.args...)
			if got.code != cli.ExitOK || got.stderr != "" {
				t.Fatalf("code %d, stderr %q; want 0 and no stderr", got.code, got.stderr)
			}
			for _, s := range []string{"--log-level string", "--log-format string", `(default "text")`} {
				if !strings.Contains(got.stdout, s) {
					t.Errorf("help lacks %q:\n%s", s, got.stdout)
				}
			}
			for _, line := range strings.Split(got.stdout, "\n") {
				if strings.Contains(line, "--log-level") && strings.Contains(line, "(default") {
					t.Errorf("--log-level shows a default: %q", line)
				}
			}
		})
	}
}

func TestWithLoggingRequiresVerboseAndQuiet(t *testing.T) {
	tests := []struct {
		name string
		opts []cli.Option
	}{
		{name: "without WithVerbose", opts: []cli.Option{cli.WithQuiet(), cli.WithLogging()}},
		{name: "without WithQuiet", opts: []cli.Option{cli.WithLogging(), cli.WithVerbose()}},
		{name: "without either", opts: []cli.Option{cli.WithLogging()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ran bool
			got := runApp(t, loggingApp(&ran, tt.opts...), "run")
			if got.code != cli.ExitError || got.stdout != "" {
				t.Errorf("code %d, stdout %q; want 1 and no stdout", got.code, got.stdout)
			}
			want := "tool: invalid command tree\n" +
				"  → WithLogging requires WithVerbose and WithQuiet\n"
			if got.stderr != want {
				t.Errorf("stderr %q, want %q", got.stderr, want)
			}
			if ran {
				t.Error("run function called, want none")
			}
		})
	}
}

func TestWithLoggingWithVerboseAndQuietRuns(t *testing.T) {
	restoreDefaultLogger(t)
	var ran bool
	app := loggingApp(&ran, cli.WithLogging(), cli.WithVerbose(), cli.WithQuiet())
	got := runApp(t, app, "run", "--log-level", "debug", "--log-format", "json")
	if got.code != cli.ExitOK || got.stdout != "" || got.stderr != "" {
		t.Errorf("result %+v, want 0 and no output", got)
	}
	if !ran {
		t.Error("run function not called")
	}
}

// restoreDefaultLogger puts back, when t ends, the default logger a WithLogging App replaces.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	saved := slog.Default()
	t.Cleanup(func() { slog.SetDefault(saved) })
}

// recordingApp returns an App taking WithLogging, -v and -q, with a "run" command that writes one
// record at each of the levels debug, info, warn and error to the default logger.
func recordingApp() *cli.App {
	app := cli.New(identity(), cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging())
	app.Add(&spec.Command{Name: "run", Run: func(ctx context.Context, _ *spec.Invocation) error {
		slog.DebugContext(ctx, "debug record")
		slog.InfoContext(ctx, "info record")
		slog.WarnContext(ctx, "warn record")
		slog.ErrorContext(ctx, "error record")
		return nil
	}})
	return app
}

// textRecord and jsonRecord return the part of a record of level that names it and its message
// in the text and the JSON format.
func textRecord(level string) string {
	return "level=" + strings.ToUpper(level) + ` msg="` + level + ` record"`
}

func jsonRecord(level string) string {
	return `"level":"` + strings.ToUpper(level) + `","msg":"` + level + ` record"`
}

func TestWithLoggingWritesRecordsToStderr(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}
	tests := []struct {
		name   string
		args   []string
		record func(string) string
		want   []string
	}{
		{name: "default", args: nil, record: textRecord,
			want: []string{"info", "warn", "error"}},
		{name: "-v", args: []string{"-v"}, record: textRecord,
			want: []string{"debug", "info", "warn", "error"}},
		{name: "--verbose", args: []string{"--verbose"}, record: textRecord,
			want: []string{"debug", "info", "warn", "error"}},
		{name: "-q", args: []string{"-q"}, record: textRecord,
			want: []string{"error"}},
		{name: "--log-level warn", args: []string{"--log-level", "warn"}, record: textRecord,
			want: []string{"warn", "error"}},
		{name: "--log-level DEBUG", args: []string{"--log-level=DEBUG"}, record: textRecord,
			want: []string{"debug", "info", "warn", "error"}},
		{name: "--log-level error", args: []string{"--log-level", "error"}, record: textRecord,
			want: []string{"error"}},
		{name: "--log-format text", args: []string{"--log-format", "text"}, record: textRecord,
			want: []string{"info", "warn", "error"}},
		{name: "json default", args: []string{"--log-format", "json"}, record: jsonRecord,
			want: []string{"info", "warn", "error"}},
		{name: "json -v", args: []string{"--log-format=json", "-v"}, record: jsonRecord,
			want: []string{"debug", "info", "warn", "error"}},
		{name: "json -q", args: []string{"-q", "--log-format", "json"}, record: jsonRecord,
			want: []string{"error"}},
		{name: "json --log-level warn", args: []string{"--log-format", "json", "--log-level", "warn"},
			record: jsonRecord, want: []string{"warn", "error"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreDefaultLogger(t)
			got := runApp(t, recordingApp(), append([]string{"run"}, tt.args...)...)
			if got.code != cli.ExitOK || got.stdout != "" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 0 and no stdout", got.code, got.stdout, got.stderr)
			}
			lines := strings.Split(strings.TrimSuffix(got.stderr, "\n"), "\n")
			if len(lines) != len(tt.want) {
				t.Fatalf("stderr holds %d records, want %d:\n%s", len(lines), len(tt.want), got.stderr)
			}
			for _, level := range levels {
				wanted := slices.Contains(tt.want, level)
				if found := strings.Contains(got.stderr, tt.record(level)); found != wanted {
					t.Errorf("%s record written %t, want %t:\n%s", level, found, wanted, got.stderr)
				}
			}
		})
	}
}

func TestWithLoggingUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "--log-level with -v", args: []string{"--log-level", "debug", "-v"},
			want: "tool: run: --log-level and --verbose cannot be used together\n"},
		{name: "--log-level=info with -v", args: []string{"--log-level=info", "-v"},
			want: "tool: run: --log-level and --verbose cannot be used together\n"},
		{name: "--log-level with -q", args: []string{"-q", "--log-level", "warn"},
			want: "tool: run: --log-level and --quiet cannot be used together\n"},
		{name: "invalid --log-level", args: []string{"--log-level", "loud"},
			want: "tool: run: --log-level must be debug, info, warn or error, not \"loud\"\n"},
		{name: "empty --log-level", args: []string{"--log-level="},
			want: "tool: run: --log-level must be debug, info, warn or error, not \"\"\n"},
		{name: "invalid --log-format", args: []string{"--log-format", "yaml"},
			want: "tool: run: --log-format must be text or json, not \"yaml\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreDefaultLogger(t)
			before := slog.Default()
			var ran bool
			app := loggingApp(&ran, cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging())
			got := runApp(t, app, append([]string{"run"}, tt.args...)...)
			if got.code != cli.ExitUsage || got.stdout != "" || got.stderr != tt.want {
				t.Errorf("result %+v, want code %d, no stdout and stderr %q", got, cli.ExitUsage, tt.want)
			}
			if ran {
				t.Error("run function called, want none")
			}
			if slog.Default() != before {
				t.Error("default logger replaced, want it left as it was")
			}
		})
	}
}

func TestLoggerFromReturnsTheInstalledDefault(t *testing.T) {
	restoreDefaultLogger(t)
	var (
		got *slog.Logger
		ok  bool
		def *slog.Logger
	)
	app := cli.New(identity(), cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging())
	app.Add(&spec.Command{Name: "run", Run: func(ctx context.Context, _ *spec.Invocation) error {
		got, ok = cli.LoggerFrom(ctx)
		def = slog.Default()
		return nil
	}})
	res := runApp(t, app, "run")
	if res.code != cli.ExitOK {
		t.Fatalf("result %+v, want 0", res)
	}
	if !ok || got == nil {
		t.Fatalf("LoggerFrom = %v, %t; want a logger and true", got, ok)
	}
	if got != def {
		t.Error("LoggerFrom returned a logger other than slog.Default")
	}
}

func TestLoggerFromWithoutWithLogging(t *testing.T) {
	if got, ok := cli.LoggerFrom(t.Context()); ok || got != nil {
		t.Errorf("LoggerFrom = %v, %t; want nil and false", got, ok)
	}
	var (
		got *slog.Logger
		ok  = true
	)
	app := cli.New(identity(), cli.WithVerbose(), cli.WithQuiet())
	app.Add(&spec.Command{Name: "run", Run: func(ctx context.Context, _ *spec.Invocation) error {
		got, ok = cli.LoggerFrom(ctx)
		return nil
	}})
	if res := runApp(t, app, "run"); res.code != cli.ExitOK {
		t.Fatalf("result %+v, want 0", res)
	}
	if ok || got != nil {
		t.Errorf("LoggerFrom = %v, %t in a run without WithLogging; want nil and false", got, ok)
	}
}

func TestWithLoggingAndWithConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		code    int
		ran     bool
	}{
		{name: "config loads", content: "port: 9090\n", code: cli.ExitOK, ran: true},
		{name: "config fails", content: "port: [1]\n", code: cli.ExitError, ran: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreDefaultLogger(t)
			before := slog.Default()
			var (
				ran       bool
				logger    *slog.Logger
				hasLogger bool
				def       *slog.Logger
				conf      toolConfig
			)
			app := cli.New(identity(), cli.WithConfig(toolDefaults()), cli.WithLogging(),
				cli.WithVerbose(), cli.WithQuiet())
			app.Add(&spec.Command{Name: "run", Run: func(ctx context.Context, _ *spec.Invocation) error {
				ran = true
				logger, hasLogger = cli.LoggerFrom(ctx)
				def = slog.Default()
				r, _ := cli.ConfigFrom[toolConfig](ctx)
				conf = r.Value()
				return nil
			}})
			got := newConfigEnv(t, tt.content).run(t, app, "run")
			if got.code != tt.code {
				t.Fatalf("result %+v, want code %d", got, tt.code)
			}
			if ran != tt.ran {
				t.Fatalf("run function called %t, want %t", ran, tt.ran)
			}
			if !tt.ran {
				if !strings.Contains(got.stderr, "config.yaml:1") {
					t.Errorf("stderr %q, want the configuration error", got.stderr)
				}
				if slog.Default() != before {
					t.Error("default logger replaced after a configuration failure, want it left as it was")
				}
				return
			}
			if conf.Port != 9090 {
				t.Errorf("run saw port %d, want 9090 from the file", conf.Port)
			}
			if !hasLogger || logger == nil {
				t.Fatalf("LoggerFrom = %v, %t; want a logger and true", logger, hasLogger)
			}
			if logger != def {
				t.Error("LoggerFrom returned a logger other than slog.Default")
			}
			if def == before {
				t.Error("default logger not replaced, want the one WithLogging installs")
			}
		})
	}
}

func TestWithLoggingRunsBeforeEveryPreRunHook(t *testing.T) {
	restoreDefaultLogger(t)
	var order []string
	app := cli.New(identity(), cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging(),
		cli.WithPreRun(func(ctx context.Context, _ *spec.Invocation, _ spec.CommandInfo) (context.Context, error) {
			slog.DebugContext(ctx, "pre-run record")
			order = append(order, "pre-run")
			return ctx, nil
		}))
	app.Add(&spec.Command{Name: "run", Run: func(context.Context, *spec.Invocation) error {
		order = append(order, "run")
		return nil
	}})
	got := runApp(t, app, "run", "-v")
	if got.code != cli.ExitOK || !strings.Contains(got.stderr, `msg="pre-run record"`) {
		t.Errorf("result %+v, want 0 and the pre-run hook's record on stderr", got)
	}
	if !slices.Equal(order, []string{"pre-run", "run"}) {
		t.Errorf("order %v, want [pre-run run]", order)
	}
}

// toolSecret is a token of a tool's own prefix, which only [cli.WithRedaction] redacts.
const toolSecret = "tkn_abcdef123456"

// secretApp returns an App taking WithLogging, -v and -q and opts, with a "run" command that logs
// toolSecret and a baseline bearer token, and writes tail, a line without its newline, to the
// writer LogWriterFrom returns.
func secretApp(tail string, opts ...cli.Option) *cli.App {
	app := cli.New(identity(), append([]cli.Option{cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging()}, opts...)...)
	app.Add(&spec.Command{Name: "run", Run: func(ctx context.Context, _ *spec.Invocation) error {
		slog.InfoContext(ctx, "token "+toolSecret+" auth Bearer basesecret99")
		if tail != "" {
			w, _ := cli.LogWriterFrom(ctx)
			_, _ = w.Write([]byte(tail))
		}
		return nil
	}})
	return app
}

func TestWithRedactionRedactsToolPrefixes(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			restoreDefaultLogger(t)
			app := secretApp("", cli.WithRedaction(logging.Redaction{Prefixes: []string{"tkn_"}}))
			got := runApp(t, app, "run", "--log-format", format)
			if got.code != cli.ExitOK {
				t.Fatalf("result %+v, want 0", got)
			}
			for _, secret := range []string{"abcdef123456", "basesecret99"} {
				if strings.Contains(got.stderr, secret) {
					t.Errorf("stderr holds %q:\n%s", secret, got.stderr)
				}
			}
			if !strings.Contains(got.stderr, "token") {
				t.Errorf("stderr lacks the record:\n%s", got.stderr)
			}
		})
	}
}

func TestWithRedactionAccumulates(t *testing.T) {
	restoreDefaultLogger(t)
	prefixes := []string{"tkn_"}
	app := secretApp("", cli.WithRedaction(logging.Redaction{Prefixes: prefixes}),
		cli.WithRedaction(logging.Redaction{Prefixes: []string{"other_"}}))
	prefixes[0] = "zzz_"
	got := runApp(t, app, "run")
	if strings.Contains(got.stderr, "abcdef123456") || strings.Contains(got.stderr, "basesecret99") {
		t.Errorf("stderr holds a secret:\n%s", got.stderr)
	}
}

func TestBaselineRedactedWithoutWithRedaction(t *testing.T) {
	restoreDefaultLogger(t)
	got := runApp(t, secretApp(""), "run")
	if strings.Contains(got.stderr, "basesecret99") {
		t.Errorf("stderr holds the baseline secret:\n%s", got.stderr)
	}
	if !strings.Contains(got.stderr, "abcdef123456") {
		t.Errorf("stderr lacks the unregistered token, want it kept:\n%s", got.stderr)
	}
}

func TestWithRedactionRequiresWithLogging(t *testing.T) {
	var ran bool
	app := loggingApp(&ran, cli.WithVerbose(), cli.WithQuiet(), cli.WithRedaction(logging.Redaction{}))
	got := runApp(t, app, "run")
	want := "tool: invalid command tree\n  → WithRedaction requires WithLogging\n"
	if got.code != cli.ExitError || got.stdout != "" || got.stderr != want {
		t.Errorf("result %+v, want code 1 and stderr %q", got, want)
	}
	if ran {
		t.Error("run function called, want none")
	}
}

func TestLogWriterFrom(t *testing.T) {
	if w, ok := cli.LogWriterFrom(t.Context()); ok || w != nil {
		t.Errorf("LogWriterFrom = %v, %t on a bare context; want nil and false", w, ok)
	}
	restoreDefaultLogger(t)
	var (
		with, without     *logging.RedactingWriter
		okWith, okWithout bool
	)
	app := loggingApp(new(bool), cli.WithVerbose(), cli.WithQuiet(), cli.WithLogging())
	app.Add(&spec.Command{Name: "probe", Run: func(ctx context.Context, _ *spec.Invocation) error {
		with, okWith = cli.LogWriterFrom(ctx)
		return nil
	}})
	plain := cli.New(identity(), cli.WithVerbose(), cli.WithQuiet())
	plain.Add(&spec.Command{Name: "probe", Run: func(ctx context.Context, _ *spec.Invocation) error {
		without, okWithout = cli.LogWriterFrom(ctx)
		return nil
	}})
	if res := runApp(t, app, "probe"); res.code != cli.ExitOK {
		t.Fatalf("result %+v, want 0", res)
	}
	if res := runApp(t, plain, "probe"); res.code != cli.ExitOK {
		t.Fatalf("result %+v, want 0", res)
	}
	if !okWith || with == nil {
		t.Errorf("LogWriterFrom = %v, %t under WithLogging; want a writer and true", with, okWith)
	}
	if okWithout || without != nil {
		t.Errorf("LogWriterFrom = %v, %t without WithLogging; want nil and false", without, okWithout)
	}
}

func TestExecuteFlushesTheLogWriter(t *testing.T) {
	restoreDefaultLogger(t)
	app := secretApp("partial Bearer basesecret99",
		cli.WithRedaction(logging.Redaction{Prefixes: []string{"tkn_"}}))
	got := runApp(t, app, "run")
	if got.code != cli.ExitOK {
		t.Fatalf("result %+v, want 0", got)
	}
	if !strings.Contains(got.stderr, "partial Bearer ") {
		t.Errorf("stderr lacks the unterminated line:\n%s", got.stderr)
	}
	if strings.Contains(got.stderr, "basesecret99") {
		t.Errorf("stderr holds the secret of the unterminated line:\n%s", got.stderr)
	}
}

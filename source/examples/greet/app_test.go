package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatecmd"
	"go.vipengele.org/update/updatetest"
)

// testVersion is the version the App under test runs as, and servedVersion the release the test
// server publishes, which is newer.
const (
	testVersion   = "v1.0.0"
	servedVersion = "v1.1.0"
)

// testDeps returns deps whose every request goes to srv, whose state lives in a temporary
// directory and whose clock is fixed, for a build at testVersion.
func testDeps(t *testing.T, srv *updatetest.Server) deps {
	t.Helper()
	id := identity()
	return deps{
		Build: buildinfo.Info{Version: testVersion, Commit: "0123456789abcdef0123"},
		Source: &update.GitHubSource{
			Owner:      id.Owner,
			Repo:       id.Repo,
			HTTPClient: srv.Client(),
			WebURL:     srv.WebURL(),
			APIURL:     srv.APIURL(),
		},
		StateDir:   t.TempDir(),
		HTTPClient: srv.Client(),
		Now:        func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

// newServer returns a server publishing servedVersion of the tool.
func newServer(t *testing.T) *updatetest.Server {
	t.Helper()
	id := identity()
	return updatetest.NewServer(t, updatetest.NewRelease(t, id.Owner, id.Repo, servedVersion, id.Name))
}

// result is what one execution returned and wrote.
type result struct {
	code   int
	stdout string
	stderr string
}

// run executes the App of deps with args, GREET_HOME set to home, no other variable set and no
// stream a terminal.
func run(t *testing.T, d deps, home string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := &spec.Env{
		Stdout:  &stdout,
		Stderr:  &stderr,
		WorkDir: t.TempDir(),
		LookupEnv: func(key string) (string, bool) {
			if key == "GREET_HOME" {
				return home, true
			}
			return "", false
		},
		Terminal: func(spec.Stream) bool { return false },
	}
	code := newApp(d).ExecuteArgs(env, args)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestHello(t *testing.T) {
	tests := []struct {
		name   string
		config string
		args   []string
		want   string
	}{
		{name: "default", args: []string{"hello"}, want: "Hello, world!\n"},
		{name: "name", args: []string{"hello", "Ada"}, want: "Hello, Ada!\n"},
		{name: "shout", args: []string{"hello", "--shout", "Ada"}, want: "HELLO, ADA!\n"},
		{name: "alias", args: []string{"hi", "Ada"}, want: "Hello, Ada!\n"},
		{name: "whisper", args: []string{"hello", "--whisper", "Ada"}, want: "hello, ada!\n"},
		{name: "whisper through the alias", args: []string{"hi", "--whisper"}, want: "hello, world!\n"},
		{name: "config file", config: "greeting: Hi\n", args: []string{"hello", "Ada"}, want: "Hi, Ada!\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.config != "" {
				path := filepath.Join(home, "config", "config.yaml")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}
			srv := newServer(t)

			got := run(t, testDeps(t, srv), home, tt.args...)
			if want := (result{code: 0, stdout: tt.want}); got != want {
				t.Errorf("ExecuteArgs = %+v, want %+v", got, want)
			}
		})
	}
}

func TestHelloRefusesTwoNames(t *testing.T) {
	got := run(t, testDeps(t, newServer(t)), t.TempDir(), "hello", "Ada", "Grace")
	if got.code != 2 {
		t.Errorf("ExecuteArgs = %+v, want exit code 2", got)
	}
}

func TestHelloRefusesShoutWithWhisper(t *testing.T) {
	for _, name := range []string{"hello", "hi"} {
		t.Run(name, func(t *testing.T) {
			got := run(t, testDeps(t, newServer(t)), t.TempDir(), name, "--shout", "--whisper")
			want := result{code: 2, stderr: "greet: hello: --shout and --whisper are mutually exclusive\n"}
			if got != want {
				t.Errorf("ExecuteArgs = %+v, want %+v", got, want)
			}
		})
	}
}

func TestUpdateCheckReportsTheNewerRelease(t *testing.T) {
	got := run(t, testDeps(t, newServer(t)), t.TempDir(), "update", "--check")
	if got.code != updatecmd.ExitUpdateAvailable {
		t.Errorf("exit code = %d, want %d; stderr %q", got.code, updatecmd.ExitUpdateAvailable, got.stderr)
	}
	want := "greet " + servedVersion + " is available (current " + testVersion + ")"
	if !strings.HasPrefix(got.stdout, want) {
		t.Errorf("stdout = %q, want it to start with %q", got.stdout, want)
	}
}

func TestVersion(t *testing.T) {
	got := run(t, testDeps(t, newServer(t)), t.TempDir(), "version")
	if want := (result{code: 0, stdout: testVersion + " build 0123456789ab\n"}); got != want {
		t.Errorf("ExecuteArgs = %+v, want %+v", got, want)
	}
}

// layoutSeenByCommand returns the paths layout a command of the App of deps receives in its
// context, through a probe command added to that App and run with no variable set.
func layoutSeenByCommand(t *testing.T, d deps) paths.Layout {
	t.Helper()
	var seen paths.Layout
	app := newApp(d)
	app.Add(&spec.Command{
		Name:    "probe",
		Summary: "Report the paths layout",
		Run: func(ctx context.Context, _ *spec.Invocation) error {
			seen = cli.PathsLayoutFrom(ctx)
			return nil
		},
	})
	var stdout, stderr bytes.Buffer
	env := &spec.Env{
		Stdout:    &stdout,
		Stderr:    &stderr,
		WorkDir:   t.TempDir(),
		LookupEnv: func(string) (string, bool) { return "", false },
		Terminal:  func(spec.Stream) bool { return false },
	}
	if code := app.ExecuteArgs(env, []string{"probe"}); code != 0 {
		t.Fatalf("ExecuteArgs = %d, want 0; stderr %q", code, stderr.String())
	}
	return seen
}

// resolvedStateDir returns the state directory greet's update configuration resolves to for an
// empty deps.StateDir, on goos, with only the variables of env set.
func resolvedStateDir(t *testing.T, goos string, env map[string]string) string {
	t.Helper()
	d := testDeps(t, newServer(t))
	d.StateDir = ""
	c := updateConfig(d)
	c.GOOS = goos
	c.Getenv = func(key string) string { return env[key] }
	c.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	r, err := c.Resolved()
	if err != nil {
		t.Fatalf("Resolved: %v", err)
	}
	return r.StateDir
}

func TestAppResolvesDirectoriesUnderTheXDGLayout(t *testing.T) {
	if got := layoutSeenByCommand(t, testDeps(t, newServer(t))); got != paths.LayoutXDG {
		t.Errorf("PathsLayoutFrom = %v, want %v", got, paths.LayoutXDG)
	}
}

func TestUpdateStateFollowsTheXDGLayoutOnDarwin(t *testing.T) {
	stateHome := t.TempDir()
	got := resolvedStateDir(t, "darwin", map[string]string{"XDG_STATE_HOME": stateHome})
	if want := filepath.Join(stateHome, identity().DirName); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}

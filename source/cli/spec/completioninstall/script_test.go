package completioninstall_test

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/commons/validate"
)

// snippetTool is a name with a '-', the one non-alphanumeric byte a slug allows.
const snippetTool = "my-tool"

// shPath returns the sh the snippet is run under, skipping the test when there is none.
func shPath(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not on PATH")
	}
	return sh
}

// runSh runs script under sh with args and exactly the variables in env, PATH included. PATH is
// an empty directory unless env sets it, so no brew is ever found and only builtins run.
func runSh(t *testing.T, script string, env map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(shPath(t), append([]string{"-c", script, "sh"}, args...)...)
	cmd.Env = []string{"PATH=" + t.TempDir()}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("running sh: %v", err)
	}
	return out.String(), errOut.String(), code
}

// probeWritable reports that dir exists and a file can be created in it, the probe Install
// makes of the Homebrew zsh directory.
func probeWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_ = f.Close()
	return true
}

// snippetRun is the script the cross-check runs: the snippet, then a call under `set -u` whose
// output is the path, followed on stderr by the exit status and COMPLETION_NOTE. The function's
// own exit status is the script's.
func snippetRun(t *testing.T) string {
	t.Helper()
	snippet, err := completioninstall.Snippet(snippetTool)
	if err != nil {
		t.Fatalf("Snippet(%q): %v", snippetTool, err)
	}
	return snippet + `
set -u
completion_target "$1"
_status=$?
printf '%s\n%s' "$_status" "$COMPLETION_NOTE" >&2
exit "$_status"
`
}

func TestSnippetSelectsTheTargetPath(t *testing.T) {
	script := snippetRun(t)
	root := os.Geteuid() == 0

	usable := t.TempDir()
	if err := os.MkdirAll(filepath.Join(usable, "share", "zsh", "site-functions"), 0o755); err != nil {
		t.Fatal(err)
	}
	absent := t.TempDir()
	unwritable := t.TempDir()
	unwritableDir := filepath.Join(unwritable, "share", "zsh", "site-functions")
	if err := os.MkdirAll(unwritableDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unwritableDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unwritableDir, 0o755) })

	type brewCase struct {
		name   string
		prefix *string
		skip   bool
	}
	str := func(s string) *string { return &s }
	brews := []brewCase{
		{name: "brew unset"},
		{name: "brew empty", prefix: str("")},
		{name: "brew usable", prefix: str(filepath.ToSlash(usable))},
		{name: "brew usable with trailing slash", prefix: str(filepath.ToSlash(usable) + "/")},
		{name: "brew usable with dot elements", prefix: str(filepath.ToSlash(usable) + "//./x/..")},
		{name: "brew directory absent", prefix: str(filepath.ToSlash(absent))},
		// [ -w ] and the probe both answer true for root, so the case shows nothing there.
		{name: "brew directory unwritable", prefix: str(filepath.ToSlash(unwritable)), skip: root},
	}
	xdgs := []struct {
		name  string
		value *string
	}{
		{name: "xdg unset"},
		{name: "xdg empty", value: str("")},
		{name: "xdg set", value: str("/xdg/base")},
		{name: "xdg with trailing slash", value: str("/xdg/base/")},
		{name: "xdg with doubled slashes and dot elements", value: str("//xdg/./a/../base//")},
		{name: "xdg relative", value: str("../rel/./xdg/")},
		{name: "xdg root", value: str("/")},
	}
	homes := []struct {
		name  string
		value *string
	}{
		{name: "home unset"},
		{name: "home empty", value: str("")},
		{name: "home set", value: str(filepath.ToSlash(t.TempDir()))},
		{name: "home with trailing slash", value: str(filepath.ToSlash(t.TempDir()) + "/")},
	}
	shells := []completioninstall.Shell{
		completioninstall.Zsh, completioninstall.Bash, completioninstall.Fish, completioninstall.PowerShell,
	}

	for _, shell := range shells {
		for _, home := range homes {
			for _, xdg := range xdgs {
				for _, brew := range brews {
					name := strings.Join([]string{string(shell), home.name, xdg.name, brew.name}, "/")
					t.Run(name, func(t *testing.T) {
						if brew.skip {
							t.Skip("[ -w ] is true for root")
						}
						vars := map[string]string{}
						env := completioninstall.Env{GOOS: "linux"}
						if home.value != nil {
							vars["HOME"] = *home.value
							env.Home = *home.value
						}
						if xdg.value != nil {
							vars["XDG_DATA_HOME"] = *xdg.value
							vars["XDG_CONFIG_HOME"] = *xdg.value
							env.XDGDataHome = *xdg.value
							env.XDGConfigHome = *xdg.value
						}
						if brew.prefix != nil {
							vars["HOMEBREW_PREFIX"] = *brew.prefix
							env.BrewPrefix = *brew.prefix
						}
						if env.BrewPrefix != "" {
							env.BrewDirUsable = probeWritable(filepath.FromSlash(completioninstall.ZshBrewDir(env.BrewPrefix)))
						}
						assertSnippetMatches(t, script, shell, vars, env)
					})
				}
			}
		}
	}
}

// assertSnippetMatches runs the snippet for shell with vars and checks it against
// Target(shell, snippetTool, env).
func assertSnippetMatches(t *testing.T, script string, shell completioninstall.Shell, vars map[string]string, env completioninstall.Env) {
	t.Helper()
	want, reason, ok := completioninstall.Target(shell, snippetTool, env)
	stdout, stderr, code := runSh(t, script, vars, string(shell))
	status, note, found := strings.Cut(stderr, "\n")
	if !found {
		t.Fatalf("snippet stderr = %q, want status and note", stderr)
	}
	if status != strconv.Itoa(code) {
		t.Fatalf("snippet reported status %s and exited %d", status, code)
	}
	if !ok {
		if reason != completioninstall.ReasonNoHome {
			t.Fatalf("Target reason = %q, want %q", reason, completioninstall.ReasonNoHome)
		}
		if code != 1 || stdout != "" || note != "" {
			t.Fatalf("snippet = (%q, note %q, exit %d), want no output, no note and exit 1", stdout, note, code)
		}
		return
	}
	if code != 0 {
		t.Fatalf("snippet exited %d, want 0 and %q", code, want.Path)
	}
	if stdout != want.Path+"\n" {
		t.Errorf("snippet path = %q, Target path = %q", strings.TrimSuffix(stdout, "\n"), want.Path)
	}
	if note != want.Note {
		t.Errorf("snippet note = %q, Target note = %q", note, want.Note)
	}
}

func TestSnippetRejectsAnUnsupportedShell(t *testing.T) {
	script := snippetRun(t)
	for _, shell := range []string{"", "pwsh", "sh", "ZSH", "zsh "} {
		t.Run(shell, func(t *testing.T) {
			if _, reason, ok := completioninstall.Target(completioninstall.Shell(shell), snippetTool,
				completioninstall.Env{GOOS: "linux", Home: "/home/u"}); ok || reason != completioninstall.ReasonUnsupportedShell {
				t.Fatalf("Target(%q) = (ok %v, %q), want %q", shell, ok, reason, completioninstall.ReasonUnsupportedShell)
			}
			stdout, stderr, code := runSh(t, script, map[string]string{"HOME": "/home/u"}, shell)
			if code != 2 || stdout != "" || stderr != "2\n" {
				t.Fatalf("snippet = (%q, stderr %q, exit %d), want no output and exit 2", stdout, stderr, code)
			}
		})
	}
}

func TestSnippetCleansPathsAsPathClean(t *testing.T) {
	snippet, err := completioninstall.Snippet(snippetTool)
	if err != nil {
		t.Fatal(err)
	}
	script := snippet + `
_ci_clean "$1"
printf '%s' "$_ci_clean_out"
`
	inputs := []string{
		"", ".", "..", "/", "//", "///a//", "a", "a/", "/a/b/", "a/./b", "a/../b", "/../a", "/a/../..",
		"../../a", "a/../../b", "./a/./", "a/b/../../..", "/a/b/c/../../d/", "*", "/a/*/b", "a b/ c ",
	}
	for _, in := range inputs {
		stdout, stderr, code := runSh(t, script, nil, in)
		if code != 0 || stderr != "" {
			t.Fatalf("_ci_clean %q: exit %d, stderr %q", in, code, stderr)
		}
		if want := path.Clean(in); stdout != want {
			t.Errorf("_ci_clean %q = %q, path.Clean = %q", in, stdout, want)
		}
	}
}

func TestSnippetRestoresTheShellState(t *testing.T) {
	snippet, err := completioninstall.Snippet(snippetTool)
	if err != nil {
		t.Fatal(err)
	}
	for _, noglob := range []string{"set +f", "set -f"} {
		script := snippet + noglob + `
IFS=:
before=$-
completion_target bash >/dev/null
[ "$IFS" = : ] || { echo "IFS changed"; exit 1; }
[ "$-" = "$before" ] || { echo "options changed from $before to $-"; exit 1; }
`
		stdout, stderr, code := runSh(t, script, map[string]string{"HOME": "/home/u"})
		if code != 0 {
			t.Errorf("%s: exit %d: %s%s", noglob, code, stdout, stderr)
		}
	}
}

func TestSnippetIsValidSh(t *testing.T) {
	snippet, err := completioninstall.Snippet(snippetTool)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shPath(t), "-n")
	cmd.Stdin = strings.NewReader(snippet)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v: %s", err, out)
	}
}

func TestSnippetNamesTheTool(t *testing.T) {
	snippet, err := completioninstall.Snippet(snippetTool)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snippet, "_ci_name='"+snippetTool+"'") {
		t.Errorf("snippet does not quote the tool name %q:\n%s", snippetTool, snippet)
	}
	if !strings.Contains(snippet, completioninstall.SnippetFunc+"() {") {
		t.Errorf("snippet does not define %s", completioninstall.SnippetFunc)
	}
}

func TestSnippetRejectsAnUnsafeName(t *testing.T) {
	for _, name := range []string{"", "Atk", "a'b", "a;b", "a b", "$(id)", "`id`", "a/b", "-a", "a-", "a.b", "a\nb"} {
		t.Run(name, func(t *testing.T) {
			snippet, err := completioninstall.Snippet(name)
			if err == nil {
				t.Fatalf("Snippet(%q) = %q, want an error", name, snippet)
			}
			if !errors.Is(err, validate.ErrInvalid) && !errors.Is(err, validate.ErrEmpty) {
				t.Errorf("Snippet(%q) error = %v, want one wrapping validate.ErrInvalid or validate.ErrEmpty", name, err)
			}
			if snippet != "" {
				t.Errorf("Snippet(%q) returned a snippet with its error", name)
			}
		})
	}
}

package buildinfo_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildAndRun builds testdata/ldflagsmain with -buildvcs=false and the given -ldflags, runs it
// and returns the two lines it prints. The build runs in the module directory, so it resolves
// the module whether or not a go.work covers it.
func buildAndRun(t *testing.T, ldflags string) (text string, js []byte) {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go command is required to build the link-time test binary: %v", err)
	}
	moduleDir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ldflagsmain")

	args := []string{"build", "-buildvcs=false", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "./buildinfo/testdata/ldflagsmain")
	build := exec.CommandContext(t.Context(), goBin, args...)
	build.Dir = moduleDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}

	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Fatalf("running %s: %v", bin, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("output has %d lines, want 2:\n%s", len(lines), out)
	}
	return lines[0], []byte(lines[1])
}

type encoded struct {
	Schema  int    `json:"schema"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Dev     bool   `json:"dev"`
}

func decode(t *testing.T, js []byte) encoded {
	t.Helper()
	var got encoded
	if err := json.Unmarshal(js, &got); err != nil {
		t.Fatalf("decoding %s: %v", js, err)
	}
	if got.Go == "" || got.OS == "" || got.Arch == "" {
		t.Errorf("go, os and arch must be set: %s", js)
	}
	return got
}

// TestLinkTimeValues builds a binary with the -X flags the package documentation names and
// checks that the linker set each variable. A renamed variable makes the linker ignore its -X
// silently, and this test fails.
func TestLinkTimeValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary with the go command")
	}
	const (
		pkg     = "go.vipengele.org/cli/core/buildinfo"
		version = "v1.2.3"
		commit  = "0123abcdef0123abcdef0123abcdef0123abcdef"
		date    = "2026-01-02T03:04:05Z"
	)
	ldflags := "-X " + pkg + ".version=" + version +
		" -X " + pkg + ".commit=" + commit +
		" -X " + pkg + ".date=" + date

	text, js := buildAndRun(t, ldflags)
	if want := "v1.2.3 build 0123abcdef01"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
	got := decode(t, js)
	if got.Schema != 1 || got.Version != version || got.Commit != commit || got.Date != date ||
		got.Dev {
		t.Errorf("JSON = %s, want schema 1, version %q, commit %q, date %q and dev false",
			js, version, commit, date)
	}
}

// TestNoLinkTimeValues builds the same binary without -X flags or VCS stamping, which leaves
// nothing to supply a version, commit or date.
func TestNoLinkTimeValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary with the go command")
	}
	text, js := buildAndRun(t, "")
	if text != "dev" {
		t.Errorf("text = %q, want %q", text, "dev")
	}
	got := decode(t, js)
	if got.Schema != 1 || got.Version != "" || got.Commit != "" || got.Date != "" || !got.Dev {
		t.Errorf("JSON = %s, want schema 1, empty version, commit and date, and dev true", js)
	}
}

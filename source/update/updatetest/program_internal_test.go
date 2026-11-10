package updatetest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// programTag holds a single quote, a double quote and a backslash, so a release program that
// quotes its tag wrongly prints another one, or does not build.
const programTag = `v1.2.3-it's-"q"-\`

// fatalfTB records a Fatalf and its message, ending its goroutine as testing.T does, instead of
// failing the test it embeds.
type fatalfTB struct {
	testing.TB
	failed  bool
	message string
}

func (f *fatalfTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.message = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func TestReleaseExecutableOffWindowsIsTheScriptForThePlatform(t *testing.T) {
	t.Parallel()
	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		got := releaseExecutable(t, programTag, goos, "arm64")
		if want := releaseScript(programTag, goos, "arm64"); !bytes.Equal(got, want) {
			t.Errorf("releaseExecutable for %s/arm64 = %q, want the script %q", goos, got, want)
		}
		if !bytes.Contains(got, []byte(`"os":"`+goos+`","arch":"arm64"`)) {
			t.Errorf("releaseExecutable for %s/arm64 = %q, want it to report %s/arm64", goos, got, goos)
		}
	}
}

func TestReleaseExecutableForWindowsIsAPortableExecutable(t *testing.T) {
	t.Parallel()
	got := releaseExecutable(t, programTag, "windows", "amd64")
	if !bytes.HasPrefix(got, []byte("MZ")) {
		t.Errorf("releaseExecutable for windows/amd64 starts %q, want the PE signature MZ", got[:min(len(got), 16)])
	}
	if !bytes.Contains(got, []byte(versionDocument(programTag, "windows", "amd64"))) {
		t.Error("releaseExecutable for windows/amd64 does not hold the windows/amd64 version document")
	}
}

func TestReleaseExecutableFailsWithTheBuildOutput(t *testing.T) {
	t.Parallel()
	f := &fatalfTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		releaseExecutable(f, programTag, "windows", "nonsense")
	}()
	<-done
	if !f.failed || !strings.Contains(f.message, "windows/nonsense") || !strings.Contains(f.message, "unsupported GOOS/GOARCH pair") {
		t.Errorf("releaseExecutable for windows/nonsense: failed = %v, message %q; want a failure carrying the go command's output", f.failed, f.message)
	}
}

func TestReleaseExecutableStepsAreTestHelpers(t *testing.T) {
	t.Parallel()

	// Each step marks itself a helper, so a failure in it names the test's own call:
	// releaseExecutable once, and ReleaseExecutable once for itself and once for the step it calls.
	for _, tc := range []struct {
		name string
		call func(tb testing.TB)
		want int64
	}{
		{"releaseExecutable", func(tb testing.TB) { releaseExecutable(tb, programTag, "linux", "amd64") }, 1},
		{"ReleaseExecutable", func(tb testing.TB) { ReleaseExecutable(tb, programTag) }, 2},
	} {
		h := &helperCountingTB{TB: t}
		tc.call(h)
		if got := h.helpers.Load(); got != tc.want {
			t.Errorf("%s called Helper %d times, want %d", tc.name, got, tc.want)
		}
	}
}

func TestBuildReleaseProgramFailsWithoutItsDirectory(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "missing")
	if b, err := buildReleaseProgram(dir, programTag, runtime.GOOS, runtime.GOARCH); err == nil {
		t.Errorf("buildReleaseProgram into a missing directory = %d bytes, nil; want an error", len(b))
	}
}

// TestReleaseProgramAnswersAsTheScript executes the program and the script, so it does not run in
// parallel: a process a parallel test forks while this one's file is open for writing inherits that
// descriptor until it execs, and the exec then fails with ETXTBSY.
func TestReleaseProgramAnswersAsTheScript(t *testing.T) {
	program, err := buildReleaseProgram(t.TempDir(), programTag, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("buildReleaseProgram: %v", err)
	}
	programPath := writeExecutable(t, program)
	scriptPath := ""
	if runtime.GOOS != "windows" {
		scriptPath = writeExecutable(t, ReleaseBinary(programTag))
	}

	doc := versionDocument(programTag, runtime.GOOS, runtime.GOARCH) + "\n"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"version", "--json"}, doc},
		{[]string{"version", "--json", "extra"}, doc},
		{nil, programTag + "\n"},
		{[]string{"version"}, programTag + "\n"},
		{[]string{"--json", "version"}, programTag + "\n"},
	} {
		if got := runExecutable(t, programPath, tc.args...); got != tc.want {
			t.Errorf("program %q prints %q, want %q", tc.args, got, tc.want)
		}
		if scriptPath == "" {
			continue
		}
		if got := runExecutable(t, scriptPath, tc.args...); got != tc.want {
			t.Errorf("script %q prints %q, want %q", tc.args, got, tc.want)
		}
	}
}

// writeExecutable writes body as an executable file of its own temporary directory, named with
// ".exe" on Windows, and returns its path.
func writeExecutable(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), binaryFileName("release", runtime.GOOS))
	if err := os.WriteFile(path, body, 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// runExecutable runs path with args and returns its standard output, failing t when it fails.
func runExecutable(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), path, args...).Output()
	if err != nil {
		t.Fatalf("running %s %q: %v", path, args, err)
	}
	return string(out)
}

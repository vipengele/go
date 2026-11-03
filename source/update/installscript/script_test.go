package installscript_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update/updatetest"
)

func TestScriptInstallsLatestReleaseAndBashCompletion(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})

	res := h.Run()
	if res.Code != 0 {
		t.Fatalf("script exited %d\nstdout:\n%s\nstderr:\n%s", res.Code, res.Stdout, res.Stderr)
	}

	if n := h.Server.Requests(updatetest.TargetLatest); n != 1 {
		t.Errorf("the latest-release page received %d requests, want 1", n)
	}

	binary := h.BinaryPath()
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
	if mode := info.Mode(); !mode.IsRegular() || mode.Perm() != 0o755 {
		t.Errorf("binary mode = %v, want a regular file with mode 0755", mode)
	}
	out, err := exec.Command(binary, "--version").Output()
	if err != nil {
		t.Fatalf("running the installed binary: %v", err)
	}
	if want := fakeToolVersion(harnessTag); string(out) != want {
		t.Errorf("installed binary --version = %q, want %q", out, want)
	}
	if got, want := dirEntries(t, h.InstallDir), []string{h.Config.BinaryName}; !slices.Equal(got, want) {
		t.Errorf("install directory holds %q, want %q", got, want)
	}

	target := h.CompletionPath(completioninstall.Bash)
	if target == "" {
		t.Fatal("Target selects no bash completion for the harness environment")
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("bash completion not installed at %s: %v", target, err)
	}
	if want := fakeCompletion(completioninstall.Bash); string(body) != want {
		t.Errorf("bash completion = %q, want %q", body, want)
	}
	if got, want := dirEntries(t, filepath.Dir(target)), []string{filepath.Base(target)}; !slices.Equal(got, want) {
		t.Errorf("completion directory holds %q, want %q", got, want)
	}

	if got := dirEntries(t, h.TmpDir); len(got) != 0 {
		t.Errorf("TMPDIR holds %q after the script exited, want nothing", got)
	}
}

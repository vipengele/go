package installscript_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/updatetest"
)

// mustRun runs the script and fails the test unless it exits 0.
func mustRun(t *testing.T, h *harness) runResult {
	t.Helper()
	res := h.Run()
	if res.Code != 0 {
		t.Fatalf("script exited %d\nstdout:\n%s\nstderr:\n%s", res.Code, res.Stdout, res.Stderr)
	}
	return res
}

// requireVersion fails the test unless the installed binary reports tag.
func requireVersion(t *testing.T, h *harness, tag string) {
	t.Helper()
	out, err := exec.Command(h.BinaryPath(), "--version").Output()
	if err != nil {
		t.Fatalf("running the installed binary: %v", err)
	}
	if want := fakeToolVersion(tag); string(out) != want {
		t.Errorf("installed binary --version = %q, want %q", out, want)
	}
}

// requireNoFile fails the test when path exists.
func requireNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s exists or is unreadable (%v), want it absent", path, err)
	}
}

// requireNoLeftovers fails the test when dir holds anything; a missing dir holds nothing.
func requireNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("%s holds %d entries, want none", dir, len(entries))
	}
}

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

func TestScriptInstallsWithWget(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{Downloader: "wget"})

	mustRun(t, h)

	if n := h.Server.Requests(updatetest.TargetLatest); n != 1 {
		t.Errorf("the latest-release page received %d requests, want 1", n)
	}
	requireVersion(t, h, harnessTag)
}

func TestScriptInstallsThePinnedVersion(t *testing.T) {
	t.Parallel()
	for _, pin := range []string{"v2.0.1", "2.0.1"} {
		t.Run(pin, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{Tag: "v2.0.1"})
			h.Env[h.envVar("VERSION")] = pin

			mustRun(t, h)

			if n := h.Server.Requests(updatetest.TargetLatest); n != 0 {
				t.Errorf("the latest-release page received %d requests, want 0", n)
			}
			requireVersion(t, h, "v2.0.1")
		})
	}
}

func TestScriptRejectsABadChecksumAndLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{ServerOptions: []updatetest.Option{updatetest.BadChecksum()}})

	res := h.Run()

	if res.Code == 0 {
		t.Errorf("script exited 0, want a failure\nstderr:\n%s", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "checksum mismatch") {
		t.Errorf("stderr = %q, want it to report the checksum mismatch", res.Stderr)
	}
	requireNoFile(t, h.BinaryPath())
	requireNoLeftovers(t, h.InstallDir)
}

func TestScriptNamesTheMissingAsset(t *testing.T) {
	t.Parallel()
	cfg, err := testConfig("", "").Resolved()
	if err != nil {
		t.Fatalf("Resolved: %v", err)
	}
	name, err := asset.Render(cfg.AssetTemplate, asset.Fields{
		Name: cfg.BinaryName, Tag: harnessTag, OS: runtime.GOOS, Arch: runtime.GOARCH, Ext: cfg.AssetExt,
	})
	if err != nil {
		t.Fatalf("rendering the asset name: %v", err)
	}
	h := newHarness(t, harnessOptions{ServerOptions: []updatetest.Option{updatetest.MissingAsset(name)}})

	res := h.Run()

	if res.Code == 0 {
		t.Errorf("script exited 0, want a failure\nstderr:\n%s", res.Stderr)
	}
	for _, want := range []string{name, harnessTag} {
		if !strings.Contains(res.Stderr, want) {
			t.Errorf("stderr = %q, want it to name %q", res.Stderr, want)
		}
	}
	requireNoFile(t, h.BinaryPath())
	requireNoLeftovers(t, h.InstallDir)
}

func TestScriptNamesTheMissingChecksumsFile(t *testing.T) {
	t.Parallel()
	cfg, err := testConfig("", "").Resolved()
	if err != nil {
		t.Fatalf("Resolved: %v", err)
	}
	name, err := asset.Render(cfg.ChecksumsAsset, asset.Fields{
		Name: cfg.BinaryName, Tag: harnessTag, OS: runtime.GOOS, Arch: runtime.GOARCH, Ext: cfg.AssetExt,
	})
	if err != nil {
		t.Fatalf("rendering the checksums name: %v", err)
	}
	h := newHarness(t, harnessOptions{ServerOptions: []updatetest.Option{updatetest.MissingAsset(name)}})

	res := h.Run()

	if res.Code == 0 {
		t.Errorf("script exited 0, want a failure\nstderr:\n%s", res.Stderr)
	}
	if want := "release " + harnessTag + " has no checksums file " + name; !strings.Contains(res.Stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", res.Stderr, want)
	}
	requireNoFile(t, h.BinaryPath())
	requireNoLeftovers(t, h.InstallDir)
}

// listedSHA256Function returns the text of the listed_sha256 shell function of script.
func listedSHA256Function(t *testing.T, script string) string {
	t.Helper()
	_, rest, ok := strings.Cut(script, "\nlisted_sha256() {\n")
	if !ok {
		t.Fatal("the script defines no listed_sha256 function")
	}
	body, _, ok := strings.Cut(rest, "\n}\n")
	if !ok {
		t.Fatal("the listed_sha256 function has no closing brace")
	}
	return "listed_sha256() {\n" + body + "\n}\n"
}

func TestListedSHA256(t *testing.T) {
	t.Parallel()
	sh := shPath(t)
	var toolDirs []string
	for _, tool := range []string{"awk", "head"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
		toolDirs = append(toolDirs, filepath.Dir(p))
	}
	function := listedSHA256Function(t, render(t, testConfig("", "")))

	const (
		a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		b = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		c = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	)
	upperA := strings.ToUpper(a)
	for _, tc := range []struct {
		name string
		file string
		want string
	}{
		{name: "single line", file: a + "  tool.tar.gz\n", want: a},
		{name: "upper-case digest", file: upperA + "  tool.tar.gz\n", want: a},
		{name: "binary-mode separator", file: a + " *tool.tar.gz\n", want: a},
		{name: "dot-slash prefix", file: a + "  ./tool.tar.gz\n", want: a},
		{name: "not listed", file: a + "  other.tar.gz\n" + b + "  tool.tar.gz.sig\n", want: ""},
		{name: "identical duplicates", file: a + "  tool.tar.gz\n" + upperA + " *tool.tar.gz\n", want: a},
		{name: "conflicting digests", file: a + "  tool.tar.gz\n" + b + "  tool.tar.gz\n", want: "conflict"},
		{name: "conflict among other entries", file: c + "  other\n" + a + "  tool.tar.gz\n" + b + "  ./tool.tar.gz\n", want: "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "checksums")
			if err := os.WriteFile(file, []byte(tc.file), 0o600); err != nil {
				t.Fatalf("writing the checksums file: %v", err)
			}
			cmd := exec.Command(sh, "-c", function+"listed_sha256 \"$1\" \"$2\"", "sh", file, "tool.tar.gz")
			cmd.Env = []string{"PATH=" + strings.Join(toolDirs, string(os.PathListSeparator))}
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("running listed_sha256: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Errorf("listed_sha256 = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScriptWarnsOnlyWhenTheInstallDirIsOffPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		onPath bool
	}{
		{name: "off PATH", onPath: false},
		{name: "on PATH", onPath: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{})
			if tc.onPath {
				h.Env["PATH"] = h.PathDir + string(os.PathListSeparator) + h.InstallDir
			}

			res := mustRun(t, h)

			if got := strings.Contains(res.Stderr, "not on PATH"); got == tc.onPath {
				t.Errorf("PATH warning present = %v, want %v\nstderr:\n%s", got, !tc.onPath, res.Stderr)
			}
		})
	}
}

func TestScriptSkipsTheCompletionWhenDisabled(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	h.Env[h.envVar(completioninstall.NoCompletionEnvSuffix)] = "1"

	mustRun(t, h)

	requireVersion(t, h, harnessTag)
	target := h.CompletionPath(completioninstall.Bash)
	if target == "" {
		t.Fatal("Target selects no bash completion for the harness environment")
	}
	requireNoFile(t, target)
}

func TestScriptWarnsWhenTheCompletionCommandFails(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{CompletionFails: true})
	h.Env["PATH"] = h.PathDir + string(os.PathListSeparator) + h.InstallDir

	res := mustRun(t, h)

	want := h.Config.BinaryName + ": warning: " + h.Config.BinaryName + " completion bash failed; skipping the shell completion"
	if !strings.Contains(res.Stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", res.Stderr, want)
	}
	requireVersion(t, h, harnessTag)
	target := h.CompletionPath(completioninstall.Bash)
	if target == "" {
		t.Fatal("Target selects no bash completion for the harness environment")
	}
	requireNoFile(t, target)
	requireNoLeftovers(t, filepath.Dir(target))
}

func TestScriptInstallsARawBinaryRelease(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{Config: testConfig("{name}-{version}-{os}-{arch}", "")})

	mustRun(t, h)

	requireVersion(t, h, harnessTag)
	if got, want := dirEntries(t, h.InstallDir), []string{h.Config.BinaryName}; !slices.Equal(got, want) {
		t.Errorf("install directory holds %q, want %q", got, want)
	}
}

func TestScriptInstallsIntoTheHomeLocalBinByDefault(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	delete(h.Env, h.envVar("INSTALL_DIR"))
	h.InstallDir = filepath.Join(h.Env["HOME"], ".local", "bin")

	mustRun(t, h)

	requireVersion(t, h, harnessTag)
	if got, want := dirEntries(t, h.InstallDir), []string{h.Config.BinaryName}; !slices.Equal(got, want) {
		t.Errorf("default install directory holds %q, want %q", got, want)
	}
}

func TestScriptFailsWithoutHomeOrAnInstallDir(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	installDirVar := h.envVar("INSTALL_DIR")
	defaultBinary := filepath.Join(h.Env["HOME"], ".local", "bin", h.Config.BinaryName)
	delete(h.Env, installDirVar)
	delete(h.Env, "HOME")

	res := h.Run()

	if res.Code == 0 {
		t.Errorf("script exited 0, want a failure\nstderr:\n%s", res.Stderr)
	}
	if want := "HOME is not set; set " + installDirVar; !strings.Contains(res.Stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", res.Stderr, want)
	}
	requireNoFile(t, defaultBinary)
	requireNoFile(t, h.BinaryPath())
}

func TestScriptWarnsWhenShellIsNotSetAndSkipsTheCompletion(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	delete(h.Env, "SHELL")

	res := mustRun(t, h)

	want := h.Config.BinaryName + ": warning: SHELL is not set; skipping the shell completion"
	if !strings.Contains(res.Stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", res.Stderr, want)
	}
	requireVersion(t, h, harnessTag)
	target := h.CompletionPath(completioninstall.Bash)
	if target == "" {
		t.Fatal("Target selects no bash completion for the harness environment")
	}
	requireNoFile(t, target)
}

package completioninstall

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

const exePath = "/opt/atk/bin/atk"

func testIdentity() spec.Identity {
	return spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
}

// scriptFor is what the fake `completion <shell>` command prints.
func scriptFor(shell string) []byte {
	return []byte("# completion for " + shell + "\n")
}

// hermetic returns Options that read the environment env, take home as the home directory, run
// a fake `completion` command and never run brew, so Install touches nothing outside home.
func hermetic(t *testing.T, home string, env map[string]string) Options {
	t.Helper()
	return Options{
		Identity:       testIdentity(),
		ExecutablePath: exePath,
		GOOS:           "linux",
		LookupEnv: func(key string) (string, bool) {
			v, ok := env[key]
			return v, ok
		},
		UserHomeDir: func() (string, error) { return home, nil },
		BrewPrefix:  func() (string, error) { return "", errors.New("brew not installed") },
		Run: func(exe string, args ...string) ([]byte, error) {
			if exe != exePath || len(args) != 2 || args[0] != "completion" {
				t.Errorf("Run(%q, %q), want Run(%q, completion <shell>)", exe, args, exePath)
				return nil, errors.New("unexpected command")
			}
			return scriptFor(args[1]), nil
		},
		Executable: func() (string, error) {
			t.Error("Executable called although ExecutablePath is set")
			return "", errors.New("unexpected call")
		},
	}
}

// forbidRun makes every step after the skip decision fail the test.
func forbidRun(t *testing.T, opts *Options) {
	t.Helper()
	opts.Run = func(string, ...string) ([]byte, error) {
		t.Error("Run called on a skipped installation")
		return nil, errors.New("unexpected call")
	}
	opts.WriteFile = func(string, []byte, fs.FileMode) error {
		t.Error("WriteFile called on a skipped installation")
		return errors.New("unexpected call")
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") || strings.Contains(e.Name(), "probe") {
			t.Errorf("leftover temporary file %s in %s", e.Name(), dir)
		}
	}
}

func TestInstallDisabledByOptionRunsNothing(t *testing.T) {
	fail := func() (string, error) {
		t.Error("seam called on a disabled installation")
		return "", errors.New("unexpected call")
	}
	opts := Options{
		Disabled: true,
		LookupEnv: func(string) (string, bool) {
			t.Error("LookupEnv called on a disabled installation")
			return "", false
		},
		UserHomeDir: fail,
		BrewPrefix:  fail,
		Executable:  fail,
	}
	forbidRun(t, &opts)

	res, err := Install(opts)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := Result{Skipped: true, Reason: ReasonDisabled}
	if res != want {
		t.Errorf("Install = %+v, want %+v", res, want)
	}
}

func TestInstallOptOutVariable(t *testing.T) {
	tests := []struct {
		value    string
		set      bool
		disabled bool
	}{
		{value: "1", set: true, disabled: true},
		{value: "0", set: true},
		{value: "", set: true},
		{value: "true", set: true},
		{value: " 1", set: true},
		{set: false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			home := t.TempDir()
			env := map[string]string{"SHELL": "/bin/bash"}
			if tt.set {
				env["ATK_NO_COMPLETION"] = tt.value
			}
			opts := hermetic(t, home, env)
			if tt.disabled {
				forbidRun(t, &opts)
			}

			res, err := Install(opts)
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			if tt.disabled {
				want := Result{Skipped: true, Reason: ReasonDisabled}
				if res != want {
					t.Errorf("Install = %+v, want %+v", res, want)
				}
				return
			}
			if res.Skipped || !res.Changed {
				t.Errorf("Install = %+v, want an installation", res)
			}
		})
	}
}

func TestInstallSkipReasons(t *testing.T) {
	tests := []struct {
		name   string
		shell  Shell
		env    map[string]string
		home   func() (string, error)
		goos   string
		want   Reason
		wantSh Shell
	}{
		{name: "SHELL unset", want: ReasonShellNotDetected},
		{name: "SHELL empty", env: map[string]string{"SHELL": ""}, want: ReasonShellNotDetected},
		{name: "SHELL names another program", env: map[string]string{"SHELL": "/bin/tcsh"}, want: ReasonUnsupportedShell},
		{name: "Shell option not a constant", shell: "tcsh", want: ReasonUnsupportedShell, wantSh: "tcsh"},
		{
			name: "home lookup fails", shell: Bash, want: ReasonNoHome, wantSh: Bash,
			home: func() (string, error) { return "", errors.New("no home") },
		},
		{
			name: "home empty", env: map[string]string{"SHELL": "/usr/bin/fish"}, want: ReasonNoHome, wantSh: Fish,
			home: func() (string, error) { return "", nil },
		},
		{name: "zsh on windows", shell: Zsh, goos: "windows", want: ReasonUnsupportedOS, wantSh: Zsh},
		{name: "bash on windows", shell: Bash, goos: "windows", want: ReasonUnsupportedOS, wantSh: Bash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := hermetic(t, t.TempDir(), tt.env)
			opts.Shell = tt.shell
			if tt.home != nil {
				opts.UserHomeDir = tt.home
			}
			if tt.goos != "" {
				opts.GOOS = tt.goos
			}
			forbidRun(t, &opts)

			res, err := Install(opts)
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			want := Result{Shell: tt.wantSh, Skipped: true, Reason: tt.want}
			if res != want {
				t.Errorf("Install = %+v, want %+v", res, want)
			}
		})
	}
}

func TestInstallPerShell(t *testing.T) {
	tests := []struct {
		name  string
		shell Shell
		env   func(home, brew string) map[string]string
		goos  string
		// brew makes the BrewPrefix seam answer the prefix; otherwise it fails.
		brew bool
		// brewDir creates the prefix's zsh directory.
		brewDir bool
		// path is the target relative to home, or to the brew prefix when inBrew.
		path   string
		inBrew bool
		note   string
	}{
		{
			name: "zsh to HOMEBREW_PREFIX", shell: Zsh, brewDir: true, inBrew: true,
			env:  func(_, brew string) map[string]string { return map[string]string{"HOMEBREW_PREFIX": brew} },
			path: "share/zsh/site-functions/_atk",
		},
		{
			name: "zsh to brew --prefix", shell: Zsh, brew: true, brewDir: true, inBrew: true,
			path: "share/zsh/site-functions/_atk",
		},
		{
			name: "zsh without a brew directory", shell: Zsh,
			env:  func(_, brew string) map[string]string { return map[string]string{"HOMEBREW_PREFIX": brew} },
			path: ".zsh/completions/_atk", note: "fpath",
		},
		{name: "zsh without brew", shell: Zsh, path: ".zsh/completions/_atk", note: "fpath"},
		{name: "bash", shell: Bash, path: ".local/share/bash-completion/completions/atk"},
		{
			name: "bash under XDG_DATA_HOME", shell: Bash,
			env:  func(home, _ string) map[string]string { return map[string]string{"XDG_DATA_HOME": home + "/xdg-data"} },
			path: "xdg-data/bash-completion/completions/atk",
		},
		{name: "fish", shell: Fish, path: ".config/fish/completions/atk.fish"},
		{
			name: "fish under XDG_CONFIG_HOME", shell: Fish,
			env: func(home, _ string) map[string]string {
				return map[string]string{"XDG_CONFIG_HOME": home + "/xdg-config"}
			},
			path: "xdg-config/fish/completions/atk.fish",
		},
		{name: "powershell", shell: PowerShell, path: ".config/powershell/Completions/atk.ps1", note: "$PROFILE"},
		{
			name: "powershell on windows", shell: PowerShell, goos: "windows",
			path: "Documents/PowerShell/Completions/atk.ps1", note: "$PROFILE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			brew := t.TempDir()
			if tt.brewDir {
				if err := os.MkdirAll(filepath.Join(brew, "share", "zsh", "site-functions"), 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
			}
			var env map[string]string
			if tt.env != nil {
				env = tt.env(home, brew)
			}
			opts := hermetic(t, home, env)
			opts.Shell = tt.shell
			if tt.goos != "" {
				opts.GOOS = tt.goos
			}
			if tt.brew {
				opts.BrewPrefix = func() (string, error) { return brew + "\n", nil }
			}

			res, err := Install(opts)
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			base := home
			if tt.inBrew {
				base = brew
			}
			wantPath := filepath.Join(base, filepath.FromSlash(tt.path))
			if res.Shell != tt.shell || res.Path != wantPath || res.Skipped || res.Reason != "" || !res.Changed {
				t.Errorf("Install = %+v, want Shell %s, Path %s, Changed", res, tt.shell, wantPath)
			}
			if tt.note == "" && res.Note != "" || !strings.Contains(res.Note, tt.note) {
				t.Errorf("Note = %q, want one containing %q", res.Note, tt.note)
			}
			if got := readString(t, wantPath); got != string(scriptFor(string(tt.shell))) {
				t.Errorf("file holds %q, want %q", got, scriptFor(string(tt.shell)))
			}
			info, err := os.Stat(wantPath)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
				t.Errorf("mode = %#o, want 0644", info.Mode().Perm())
			}
			assertNoTempFiles(t, filepath.Dir(wantPath))
			if tt.brewDir {
				assertNoTempFiles(t, filepath.Join(brew, "share", "zsh", "site-functions"))
			}
		})
	}
}

func TestInstallLooksUpBrewOnlyForZshWithoutHomebrewPrefix(t *testing.T) {
	for _, tt := range []struct {
		shell Shell
		env   map[string]string
	}{
		{shell: Bash},
		{shell: Fish},
		{shell: PowerShell},
		{shell: Zsh, env: map[string]string{"HOMEBREW_PREFIX": "/nonexistent/brew"}},
	} {
		t.Run(string(tt.shell), func(t *testing.T) {
			opts := hermetic(t, t.TempDir(), tt.env)
			opts.Shell = tt.shell
			opts.BrewPrefix = func() (string, error) {
				t.Error("BrewPrefix called")
				return "", errors.New("unexpected call")
			}
			if _, err := Install(opts); err != nil {
				t.Fatalf("Install: %v", err)
			}
		})
	}
}

func TestInstallDetectsShellFromSHELL(t *testing.T) {
	home := t.TempDir()
	opts := hermetic(t, home, map[string]string{"SHELL": "/usr/local/bin/fish"})

	res, err := Install(opts)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Shell != Fish || res.Path != filepath.Join(home, ".config", "fish", "completions", "atk.fish") {
		t.Errorf("Install = %+v, want fish installed under %s", res, home)
	}
}

func TestInstallLocatesTheRunningExecutable(t *testing.T) {
	opts := hermetic(t, t.TempDir(), nil)
	opts.Shell = Bash
	opts.ExecutablePath = ""
	opts.Executable = func() (string, error) { return exePath, nil }
	if _, err := Install(opts); err != nil {
		t.Fatalf("Install: %v", err)
	}

	opts.Executable = func() (string, error) { return "", errors.New("no executable") }
	opts.WriteFile = func(string, []byte, fs.FileMode) error {
		t.Error("WriteFile called without an executable")
		return nil
	}
	if _, err := Install(opts); err == nil || !strings.Contains(err.Error(), "no executable") {
		t.Errorf("Install error = %v, want the Executable failure", err)
	}
}

func TestInstallGenerationFailureLeavesTargetUntouched(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".config", "fish", "completions", "atk.fish")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(target, []byte("old script"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opts := hermetic(t, home, nil)
	opts.Shell = Fish
	opts.Run = func(string, ...string) ([]byte, error) {
		return []byte("partial"), errors.New("exit status 1: unknown command \"completion\"")
	}

	res, err := Install(opts)
	if err == nil {
		t.Fatal("Install succeeded with a failing completion command")
	}
	if !strings.Contains(err.Error(), "core.WithCompletion()") || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("Install error = %v, want one naming core.WithCompletion() and the cause", err)
	}
	if res.Changed {
		t.Errorf("Changed = true after a failed generation")
	}
	if got := readString(t, target); got != "old script" {
		t.Errorf("target holds %q, want it untouched", got)
	}
}

func TestInstallIdenticalScriptIsNotRewritten(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".local", "share", "bash-completion", "completions", "atk")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(target, scriptFor("bash"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opts := hermetic(t, home, nil)
	opts.Shell = Bash
	opts.WriteFile = func(string, []byte, fs.FileMode) error {
		t.Error("WriteFile called for identical content")
		return nil
	}

	res, err := Install(opts)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Changed || res.Skipped || res.Path != target {
		t.Errorf("Install = %+v, want Path %s and Changed false", res, target)
	}
}

func TestInstallDifferentScriptIsRewritten(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".local", "share", "bash-completion", "completions", "atk")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(target, []byte("old script"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opts := hermetic(t, home, nil)
	opts.Shell = Bash

	res, err := Install(opts)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Changed {
		t.Errorf("Changed = false for different content")
	}
	if got := readString(t, target); got != string(scriptFor("bash")) {
		t.Errorf("target holds %q, want the new script", got)
	}
}

func TestInstallWritesOverAnUnreadableTarget(t *testing.T) {
	home := t.TempDir()
	opts := hermetic(t, home, nil)
	opts.Shell = Bash
	opts.readFile = func(string) ([]byte, error) {
		// The content an unreadable file would hold, had it been readable.
		return scriptFor("bash"), fs.ErrPermission
	}
	var wrote []string
	opts.WriteFile = func(path string, data []byte, perm fs.FileMode) error {
		wrote = append(wrote, path)
		if perm != 0o644 || !bytes.Equal(data, scriptFor("bash")) {
			t.Errorf("WriteFile(%s, %q, %#o), want the script at 0644", path, data, perm)
		}
		return nil
	}

	res, err := Install(opts)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Changed || !slices.Equal(wrote, []string{res.Path}) {
		t.Errorf("Install = %+v with writes %q, want one write of Path", res, wrote)
	}
}

func TestInstallWriteFailureIsAnError(t *testing.T) {
	opts := hermetic(t, t.TempDir(), nil)
	opts.Shell = Fish
	opts.WriteFile = func(string, []byte, fs.FileMode) error { return errors.New("disk full") }

	res, err := Install(opts)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Install error = %v, want the write failure", err)
	}
	if res.Changed || res.Path == "" {
		t.Errorf("Install = %+v, want Path set and Changed false", res)
	}
}

func TestInstallRejectsAnInvalidIdentity(t *testing.T) {
	opts := hermetic(t, t.TempDir(), map[string]string{"SHELL": "/bin/bash"})
	opts.Identity.EnvPrefix = ""
	forbidRun(t, &opts)

	if _, err := Install(opts); err == nil || !strings.Contains(err.Error(), "invalid identity") {
		t.Errorf("Install error = %v, want an invalid identity", err)
	}
}

func TestWriteFileAtomicCreatesParentsAndOverwrites(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a", "b", "atk")

	if err := writeFileAtomic(target, []byte("one"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	if got := readString(t, target); got != "one" {
		t.Errorf("target holds %q, want %q", got, "one")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %#o, want 0644", info.Mode().Perm())
	}

	if err := writeFileAtomic(target, []byte("two"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic over an existing file: %v", err)
	}
	if got := readString(t, target); got != "two" {
		t.Errorf("target holds %q, want %q", got, "two")
	}
	assertNoTempFiles(t, filepath.Dir(target))
}

func TestWriteFileAtomicRemovesTheTempFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "atk")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	steps := realSteps()
	var renamed string
	steps.rename = func(oldpath, _ string) error {
		renamed = oldpath
		return errors.New("rename refused")
	}

	err := steps.write(target, []byte("new"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "rename refused") {
		t.Fatalf("write error = %v, want the rename failure", err)
	}
	if filepath.Dir(renamed) != dir {
		t.Errorf("temporary file %s is not in the target directory %s", renamed, dir)
	}
	if _, err := os.Stat(renamed); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temporary file %s remains: %v", renamed, err)
	}
	assertNoTempFiles(t, dir)
	if got := readString(t, target); got != "old" {
		t.Errorf("target holds %q, want it untouched", got)
	}
}

func TestWriteFileAtomicFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := writeFileAtomic(filepath.Join(blocker, "sub", "atk"), []byte("x"), 0o644); err == nil {
		t.Fatal("writeFileAtomic succeeded under a regular file")
	}
	assertNoTempFiles(t, root)
}

func TestDirWritable(t *testing.T) {
	dir := t.TempDir()
	if !dirWritable(dir) {
		t.Errorf("dirWritable(%s) = false for a writable directory", dir)
	}
	assertNoTempFiles(t, dir)

	if dirWritable(filepath.Join(dir, "missing")) {
		t.Error("dirWritable = true for a missing directory")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if dirWritable(file) {
		t.Error("dirWritable = true for a regular file")
	}
}

func TestDirWritableReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory's mode bits do not restrict writes on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if dirWritable(dir) {
		t.Error("dirWritable = true for a read-only directory")
	}
}

func TestRunReportsANonZeroExitWithItsStderr(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	out, err := run(sh, "-c", "echo script")
	if err != nil || string(out) != "script\n" {
		t.Errorf("run = %q, %v, want the standard output", out, err)
	}
	if _, err := run(sh, "-c", "echo 'unknown command' >&2; exit 3"); err == nil ||
		!strings.Contains(err.Error(), "unknown command") {
		t.Errorf("run error = %v, want the exit carrying stderr", err)
	}
	if _, err := run(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("run succeeded for a missing executable")
	}
}

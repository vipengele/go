package installscript_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/updatetest"
)

// powerShells returns the path of each PowerShell the script is run under, keyed by a name for
// the subtest: Windows PowerShell 5.1 and PowerShell 7, each when installed. It skips the test when
// neither is.
func powerShells(t *testing.T) map[string]string {
	t.Helper()
	shells := map[string]string{}
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		shells["powershell"] = p
	} else if p := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"); fileExists(p) {
		shells["powershell"] = p
	}
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		shells["pwsh"] = p
	}
	if len(shells) == 0 {
		t.Skip("neither powershell.exe nor pwsh.exe is installed")
	}
	return shells
}

// forEachPowerShell runs test as a subtest for each PowerShell powerShells finds, in parallel
// when parallel is true.
func forEachPowerShell(t *testing.T, parallel bool, test func(t *testing.T, shell string)) {
	t.Helper()
	shells := powerShells(t)
	names := make([]string, 0, len(shells))
	for name := range shells {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if parallel {
				t.Parallel()
			}
			test(t, shells[name])
		})
	}
}

// fileExists reports whether path names something that exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fakeToolExe returns the path of the fake tool TestMain built.
func fakeToolExe(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(fakeToolEnv)
	if bin == "" {
		t.Fatalf("%s is unset: TestMain did not build the fake tool", fakeToolEnv)
	}
	return bin
}

// psOptions selects the release a psHarness publishes and the environment the script runs in.
// The zero value publishes [harnessTag] of testConfig("", ""), a zip archive, with the minisign
// stand-in on PATH and a script that trusts the server's key.
type psOptions struct {
	// Config is the tool the script is rendered for; a Config with no Name is testConfig("", "").
	// Unless it sets AllowUnsigned, the script trusts the server's key alone.
	Config update.Config
	// ServerOptions are the faults the release server is started with.
	ServerOptions []updatetest.Option
	// NoMinisign leaves the minisign stand-in off PATH.
	NoMinisign bool
	// InstallDir is the install directory, relative to the test's temporary directory; "" is
	// install\bin.
	InstallDir string
}

// psHarness is one run of the PowerShell install script against a fake release server.
type psHarness struct {
	t *testing.T
	// Shell is the PowerShell the script runs under.
	Shell string
	// Config is the Config the script is rendered from, resolved for windows.
	Config update.Config
	// Server is the fake GitHub the script's base URL points at.
	Server *updatetest.Server
	// Script is the rendered install script.
	Script string
	// InstallDir is the directory the script is told to install into; it does not exist before
	// the script runs.
	InstallDir string
	// Home is the script's USERPROFILE and HOME.
	Home string
	// TmpDir is the script's TEMP and TMP.
	TmpDir string
	// Env holds the variables Run sets on top of the test's environment, replacing any of the same
	// name; deleting a key leaves that variable out. PATH holds the system directories alone, and
	// the minisign stand-in's unless NoMinisign.
	Env map[string]string
}

// newPSHarness starts the server publishing the fake tool for windows and the running GOARCH,
// renders the script for opts.Config and builds the environment: USERPROFILE, HOME, TEMP, TMP and
// the install directory under the test's temporary directory, the base URL naming the server and
// <PREFIX>_NO_MODIFY_PATH set to 1, so no test but the one about PATH edits the user's registry.
func newPSHarness(t *testing.T, shell string, opts psOptions) *psHarness {
	t.Helper()
	if opts.Config.Name == "" {
		opts.Config = testConfig("", "")
	}
	if opts.InstallDir == "" {
		opts.InstallDir = filepath.Join("install", "bin")
	}
	resolvable := opts.Config
	resolvable.GOOS = "windows"
	cfg, err := resolvable.Resolved()
	if err != nil {
		t.Fatalf("Resolved: %v", err)
	}
	root := t.TempDir()
	h := &psHarness{
		t:          t,
		Shell:      shell,
		Config:     cfg,
		InstallDir: filepath.Join(root, opts.InstallDir),
		Home:       mkdir(t, root, "home"),
		TmpDir:     mkdir(t, root, "tmp"),
	}
	h.Server = updatetest.NewServer(t, fakeWindowsRelease(t, cfg), opts.ServerOptions...)
	if !opts.Config.AllowUnsigned {
		opts.Config.TrustedKeys = []string{h.Server.PublicKey()}
	}
	h.Script = renderPowerShell(t, opts.Config)

	systemRoot := os.Getenv("SystemRoot")
	path := []string{
		filepath.Join(systemRoot, "System32"),
		systemRoot,
		filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0"),
	}
	if !opts.NoMinisign {
		path = append([]string{filepath.Dir(fakeMinisign(t))}, path...)
	}
	h.Env = map[string]string{
		"PATH":        strings.Join(path, ";"),
		"USERPROFILE": h.Home,
		"HOME":        h.Home,
		"TEMP":        h.TmpDir,
		"TMP":         h.TmpDir,
	}
	h.Env[h.envVar("BASE_URL")] = h.Server.WebURL()
	h.Env[h.envVar("INSTALL_DIR")] = h.InstallDir
	h.Env[h.envVar("NO_MODIFY_PATH")] = "1"
	return h
}

// fakeWindowsRelease returns the release tagged harnessTag of the tool cfg describes for windows
// and the running GOARCH: its asset named by cfg's AssetTemplate, a zip or tar.gz archive holding
// the fake tool as <BinaryName>.exe in a subdirectory when the template holds {ext}, and the fake
// tool itself when it does not.
func fakeWindowsRelease(t *testing.T, cfg update.Config) updatetest.Release {
	t.Helper()
	fields := asset.Fields{Name: cfg.BinaryName, Tag: harnessTag, OS: "windows", Arch: runtime.GOARCH, Ext: cfg.AssetExt}
	assetName, err := asset.Render(cfg.AssetTemplate, fields)
	if err != nil {
		t.Fatalf("rendering the asset name: %v", err)
	}
	checksumsName, err := asset.Render(cfg.ChecksumsAsset, fields)
	if err != nil {
		t.Fatalf("rendering the checksums name: %v", err)
	}
	body, err := os.ReadFile(fakeToolExe(t))
	if err != nil {
		t.Fatalf("reading the fake tool: %v", err)
	}
	if asset.UsesExt(cfg.AssetTemplate) {
		// tgz and tar.gz name the same format, which BuildArchive knows by ExtTarGz alone.
		ext := updatetest.ExtTarGz
		if cfg.AssetExt == "zip" {
			ext = updatetest.ExtZip
		}
		body, err = updatetest.BuildArchive(ext, []updatetest.Entry{
			{Name: "bin/" + cfg.BinaryName + ".exe", Mode: 0o755, Body: body},
		})
		if err != nil {
			t.Fatalf("building the archive: %v", err)
		}
	}
	return updatetest.Release{
		Owner:         cfg.Owner,
		Repo:          cfg.Repo,
		Tag:           harnessTag,
		ArchiveName:   assetName,
		Archive:       body,
		ChecksumsName: checksumsName,
	}
}

// envVar returns the name of the variable the script reads for suffix.
func (h *psHarness) envVar(suffix string) string {
	h.t.Helper()
	name, err := h.Config.EnvVar(suffix)
	if err != nil {
		h.t.Fatalf("EnvVar(%q): %v", suffix, err)
	}
	return name
}

// BinaryPath returns where the script places the binary.
func (h *psHarness) BinaryPath() string {
	return filepath.Join(h.InstallDir, h.Config.BinaryName+".exe")
}

// CompletionPath returns the file [completioninstall.Target] selects for PowerShell on windows
// with the harness's home directory.
func (h *psHarness) CompletionPath() string {
	h.t.Helper()
	loc, _, ok := completioninstall.Target(completioninstall.PowerShell, h.Config.BinaryName, completioninstall.Env{
		GOOS: "windows",
		Home: filepath.ToSlash(h.Home),
	})
	if !ok {
		h.t.Fatal("Target selects no PowerShell completion for the harness environment")
	}
	return filepath.FromSlash(loc.Path)
}

// Run runs the script as `irm <url> | iex` does, through iex in a non-interactive PowerShell,
// with the test's environment overlaid with Env, failing the test when the shell cannot be started
// or the script outlives scriptTimeout. Output joins stdout and stderr: where PowerShell writes
// a warning or an error record depends on its version.
func (h *psHarness) Run() runResult {
	h.t.Helper()
	script := filepath.Join(h.t.TempDir(), "install.ps1")
	if err := os.WriteFile(script, []byte(h.Script), 0o644); err != nil {
		h.t.Fatalf("writing the script: %v", err)
	}
	ctx, cancel := context.WithTimeout(h.t.Context(), scriptTimeout)
	defer cancel()

	command := "iex (Get-Content -Raw -Encoding UTF8 -LiteralPath '" + strings.ReplaceAll(script, "'", "''") + "')"
	cmd := exec.CommandContext(ctx, h.Shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", command)
	cmd.Dir = h.TmpDir
	cmd.Env = h.environ()
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		h.t.Fatalf("the script did not finish within %v\nstdout:\n%s\nstderr:\n%s", scriptTimeout, stdout.String(), stderr.String())
	}
	res := runResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		res.Code = exitErr.ExitCode()
	case err != nil:
		h.t.Fatalf("running the script: %v", err)
	}
	return res
}

// environ returns the test's environment with every variable Env names, every variable of the
// tool's prefix and PSModulePath removed, then Env's values added. PSModulePath goes because each
// PowerShell builds its own, and one inherited from the other edition loads modules it cannot run.
func (h *psHarness) environ() []string {
	prefix := strings.ToUpper(h.Config.EnvPrefix + "_")
	drop := func(name string) bool {
		if strings.EqualFold(name, "PSModulePath") || strings.HasPrefix(strings.ToUpper(name), prefix) {
			return true
		}
		for k := range h.Env {
			if strings.EqualFold(k, name) {
				return true
			}
		}
		return false
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		// A name starting with "=", such as "=C:", is cmd.exe's record of a drive's directory.
		if name != "" && drop(name) {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range h.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// output returns everything the script printed.
func (r runResult) output() string {
	return r.Stdout + r.Stderr
}

// mustRunPS runs the script and fails the test unless it exits 0.
func mustRunPS(t *testing.T, h *psHarness) runResult {
	t.Helper()
	res := h.Run()
	if res.Code != 0 {
		t.Fatalf("script exited %d\nstdout:\n%s\nstderr:\n%s", res.Code, res.Stdout, res.Stderr)
	}
	return res
}

// requireInstalled fails the test unless the install directory holds the binary alone and the
// binary reports harnessTag.
func requireInstalled(t *testing.T, h *psHarness) {
	t.Helper()
	out, err := exec.Command(h.BinaryPath(), "--version").Output()
	if err != nil {
		t.Fatalf("running the installed binary: %v", err)
	}
	if want := fakeToolVersion(harnessTag); string(out) != want {
		t.Errorf("installed binary --version = %q, want %q", out, want)
	}
	if got, want := dirEntries(t, h.InstallDir), []string{h.Config.BinaryName + ".exe"}; !slices.Equal(got, want) {
		t.Errorf("install directory holds %q, want %q", got, want)
	}
}

// requireNothingPlaced fails the test when the install directory exists.
func requireNothingPlaced(t *testing.T, h *psHarness) {
	t.Helper()
	requireNoFile(t, h.InstallDir)
}

// requireNoScriptTemp fails the test when TEMP holds a directory of the script's own: the script
// names its temporary directory after the tool. PowerShell may leave files of its own there.
func requireNoScriptTemp(t *testing.T, h *psHarness) {
	t.Helper()
	for _, name := range dirEntries(t, h.TmpDir) {
		if strings.HasPrefix(name, h.Config.BinaryName+"-") {
			t.Errorf("TEMP holds %s after the script exited", name)
		}
	}
}

// completionBody returns the completion file at path with a UTF-8 byte order mark and CRLF line
// endings, which Windows PowerShell 5.1's Set-Content writes, removed.
func completionBody(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("PowerShell completion not installed at %s: %v", path, err)
	}
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	return strings.ReplaceAll(string(body), "\r\n", "\n")
}

func TestPowerShellScriptInstallsEachAssetFormat(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext string
	}{
		{"zip by default", "", ""},
		{"tar.gz", "{name}_{os}_{arch}.{ext}", "tar.gz"},
		{"tgz", "{name}_{os}_{arch}.{ext}", "tgz"},
		{"raw", "{name}_{os}_{arch}", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			forEachPowerShell(t, true, func(t *testing.T, shell string) {
				h := newPSHarness(t, shell, psOptions{Config: testConfig(tt.tmpl, tt.ext)})
				res := mustRunPS(t, h)

				requireInstalled(t, h)
				if want := fakeCompletion(completioninstall.PowerShell); completionBody(t, h.CompletionPath()) != want {
					t.Errorf("PowerShell completion = %q, want %q", completionBody(t, h.CompletionPath()), want)
				}
				if n := h.Server.Requests(updatetest.TargetSignature); n != 1 {
					t.Errorf("the signature received %d requests, want 1", n)
				}
				if !strings.Contains(res.output(), "is not on PATH") {
					t.Errorf("the script did not warn that the install directory is not on PATH:\n%s", res.output())
				}
				requireNoScriptTemp(t, h)
			})
		})
	}
}

func TestPowerShellScriptInstallsIntoADirectoryWithSpaces(t *testing.T) {
	t.Parallel()
	forEachPowerShell(t, true, func(t *testing.T, shell string) {
		h := newPSHarness(t, shell, psOptions{InstallDir: filepath.Join("Program Files (test)", "my tool")})
		mustRunPS(t, h)
		requireInstalled(t, h)
	})
}

func TestPowerShellScriptInstallsThePinnedVersion(t *testing.T) {
	t.Parallel()
	forEachPowerShell(t, true, func(t *testing.T, shell string) {
		h := newPSHarness(t, shell, psOptions{})
		h.Env[h.envVar("VERSION")] = strings.TrimPrefix(harnessTag, "v")
		mustRunPS(t, h)
		if n := h.Server.Requests(updatetest.TargetLatest); n != 0 {
			t.Errorf("the latest-release page received %d requests, want none", n)
		}
		requireInstalled(t, h)
	})
}

func TestPowerShellScriptReplacesAnInstalledBinary(t *testing.T) {
	t.Parallel()
	forEachPowerShell(t, true, func(t *testing.T, shell string) {
		h := newPSHarness(t, shell, psOptions{})
		h.Env[h.envVar("NO_COMPLETION")] = "1"
		mustRunPS(t, h)
		if err := os.WriteFile(h.BinaryPath()+".old", []byte("stale"), 0o755); err != nil {
			t.Fatalf("writing a stale backup: %v", err)
		}
		mustRunPS(t, h)
		requireInstalled(t, h)
	})
}

func TestPowerShellScriptRefusesAChecksumMismatch(t *testing.T) {
	t.Parallel()
	forEachPowerShell(t, true, func(t *testing.T, shell string) {
		h := newPSHarness(t, shell, psOptions{ServerOptions: []updatetest.Option{updatetest.BadChecksum()}})
		res := h.Run()
		if res.Code == 0 {
			t.Fatalf("script exited 0 for a checksum mismatch\n%s", res.output())
		}
		if !strings.Contains(res.output(), "checksum mismatch for") {
			t.Errorf("the script did not report the checksum mismatch:\n%s", res.output())
		}
		requireNothingPlaced(t, h)
		requireNoScriptTemp(t, h)
	})
}

func TestPowerShellScriptRefusesASignatureThatDoesNotVerify(t *testing.T) {
	t.Parallel()
	forEachPowerShell(t, true, func(t *testing.T, shell string) {
		h := newPSHarness(t, shell, psOptions{ServerOptions: []updatetest.Option{updatetest.BadSignature()}})
		res := h.Run()
		if res.Code == 0 {
			t.Fatalf("script exited 0 for a bad signature\n%s", res.output())
		}
		if !strings.Contains(res.output(), "does not verify") {
			t.Errorf("the script did not report the failed verification:\n%s", res.output())
		}
		requireNothingPlaced(t, h)
	})
}

func TestPowerShellScriptWithoutMinisignNeedsTheOptIn(t *testing.T) {
	t.Parallel()
	t.Run("refused without it", func(t *testing.T) {
		t.Parallel()
		forEachPowerShell(t, true, func(t *testing.T, shell string) {
			h := newPSHarness(t, shell, psOptions{NoMinisign: true})
			res := h.Run()
			if res.Code == 0 {
				t.Fatalf("script exited 0 with no minisign, no opt-in and no terminal\n%s", res.output())
			}
			allow := h.envVar("ALLOW_UNSIGNED")
			for _, want := range []string{"minisign is not installed", "no terminal to ask", allow + "=1"} {
				if !strings.Contains(res.output(), want) {
					t.Errorf("the output does not mention %q:\n%s", want, res.output())
				}
			}
			requireNothingPlaced(t, h)
		})
	})
	t.Run("installed with it", func(t *testing.T) {
		t.Parallel()
		forEachPowerShell(t, true, func(t *testing.T, shell string) {
			h := newPSHarness(t, shell, psOptions{NoMinisign: true})
			h.Env[h.envVar("ALLOW_UNSIGNED")] = "1"
			res := mustRunPS(t, h)
			if !strings.Contains(res.output(), "installing on the checksum alone") {
				t.Errorf("the script did not warn that it installs on the checksum alone:\n%s", res.output())
			}
			requireInstalled(t, h)
		})
	})
}

func TestPowerShellScriptForUnsignedReleasesFetchesNoSignature(t *testing.T) {
	t.Parallel()
	forEachPowerShell(t, true, func(t *testing.T, shell string) {
		h := newPSHarness(t, shell, psOptions{Config: unsignedConfig("", ""), NoMinisign: true})
		res := mustRunPS(t, h)
		if n := h.Server.Requests(updatetest.TargetSignature); n != 0 {
			t.Errorf("the signature received %d requests, want none", n)
		}
		if strings.Contains(res.output(), "minisign") {
			t.Errorf("the unsigned script mentions minisign:\n%s", res.output())
		}
		requireInstalled(t, h)
	})
}

func TestPowerShellScriptSkipsTheCompletionWhenAskedTo(t *testing.T) {
	t.Parallel()
	forEachPowerShell(t, true, func(t *testing.T, shell string) {
		h := newPSHarness(t, shell, psOptions{})
		h.Env[h.envVar("NO_COMPLETION")] = "1"
		mustRunPS(t, h)
		requireInstalled(t, h)
		requireNoFile(t, h.CompletionPath())
	})
}

// userPath is the raw Path value of HKEY_CURRENT_USER\Environment.
type userPath struct {
	value  string
	kind   uint32
	exists bool
}

// readUserPath returns the user's Path value unexpanded, as the registry holds it.
func readUserPath(t *testing.T) userPath {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("opening HKCU\\Environment: %v", err)
	}
	defer func() {
		if err := key.Close(); err != nil {
			t.Errorf("closing HKCU\\Environment: %v", err)
		}
	}()
	value, kind, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return userPath{}
	}
	if err != nil {
		t.Fatalf("reading HKCU\\Environment\\Path: %v", err)
	}
	return userPath{value: value, kind: kind, exists: true}
}

// writeUserPath sets the user's Path value to p, deleting it when p does not exist.
func writeUserPath(t *testing.T, p userPath) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.SET_VALUE)
	if err != nil {
		t.Fatalf("opening HKCU\\Environment: %v", err)
	}
	defer func() {
		if err := key.Close(); err != nil {
			t.Errorf("closing HKCU\\Environment: %v", err)
		}
	}()
	switch {
	case !p.exists:
		err = key.DeleteValue("Path")
		if errors.Is(err, registry.ErrNotExist) {
			err = nil
		}
	case p.kind == registry.EXPAND_SZ:
		err = key.SetExpandStringValue("Path", p.value)
	default:
		err = key.SetStringValue("Path", p.value)
	}
	if err != nil {
		t.Fatalf("writing HKCU\\Environment\\Path: %v", err)
	}
}

// TestPowerShellScriptAddsTheInstallDirToTheUserPath edits the real user PATH in the registry,
// restoring it when the test ends, so it runs only on a CI runner and never in parallel with
// another test that could read it.
func TestPowerShellScriptAddsTheInstallDirToTheUserPath(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("edits the user PATH in the registry; runs only when GITHUB_ACTIONS=true")
	}
	saved := readUserPath(t)
	t.Cleanup(func() { writeUserPath(t, saved) })

	const existing = `%SystemRoot%\installscript-test-entry;`
	for _, tt := range []struct {
		name     string
		before   userPath
		noModify bool
		// want returns the Path value expected after the run, given the install directory.
		want func(dir string) userPath
	}{
		{
			name:   "appended to an expandable value",
			before: userPath{value: existing, kind: registry.EXPAND_SZ, exists: true},
			want: func(dir string) userPath {
				return userPath{value: existing + dir, kind: registry.EXPAND_SZ, exists: true}
			},
		},
		{
			name:   "appended after a separator",
			before: userPath{value: strings.TrimSuffix(existing, ";"), kind: registry.EXPAND_SZ, exists: true},
			want: func(dir string) userPath {
				return userPath{value: existing + dir, kind: registry.EXPAND_SZ, exists: true}
			},
		},
		{
			name:   "created expandable when absent",
			before: userPath{},
			want: func(dir string) userPath {
				return userPath{value: dir, kind: registry.EXPAND_SZ, exists: true}
			},
		},
		{
			name:     "left alone when asked to",
			before:   userPath{value: existing, kind: registry.EXPAND_SZ, exists: true},
			noModify: true,
			want: func(string) userPath {
				return userPath{value: existing, kind: registry.EXPAND_SZ, exists: true}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			forEachPowerShell(t, false, func(t *testing.T, shell string) {
				writeUserPath(t, tt.before)
				h := newPSHarness(t, shell, psOptions{})
				h.Env[h.envVar("NO_COMPLETION")] = "1"
				if !tt.noModify {
					delete(h.Env, h.envVar("NO_MODIFY_PATH"))
				}
				res := mustRunPS(t, h)
				requireInstalled(t, h)

				got, want := readUserPath(t), tt.want(h.InstallDir)
				if got.exists != want.exists || got.kind != want.kind || !strings.EqualFold(got.value, want.value) {
					t.Errorf("user Path = %+v, want %+v\n%s", got, want, res.output())
				}
				if tt.noModify {
					if !strings.Contains(res.output(), "is not on PATH") {
						t.Errorf("the script did not warn that the install directory is not on PATH:\n%s", res.output())
					}
				} else if !strings.Contains(res.output(), "to the user PATH") {
					t.Errorf("the script did not say it edited the user PATH:\n%s", res.output())
				}
			})
		})
	}
}

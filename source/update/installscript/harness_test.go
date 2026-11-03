package installscript_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/updatetest"
)

// harnessTag is the tag of the release a harness publishes unless its options name another.
const harnessTag = "v1.2.3"

// scriptTimeout bounds one run of the install script, so a script waiting on a server that never
// answers fails the test instead of hanging it.
const scriptTimeout = 2 * time.Minute

// noTool, as a harness's Downloader or Hasher, leaves every candidate for that role off PathDir.
const noTool = "none"

// scriptTools returns the external commands the install script and the completion snippet run,
// other than the downloader and the hasher. printf is among them: `find -exec printf` runs the
// command, not the shell builtin. brew is not, so the zsh target never depends on the host.
func scriptTools() []string {
	return []string{
		"sh", "uname", "tar", "gzip", "mktemp", "find", "mv", "cp", "chmod", "mkdir", "rm",
		"basename", "dirname", "awk", "head", "cat", "printf",
	}
}

// harnessOptions selects the release a harness publishes and the host the script runs on. The
// zero value publishes [harnessTag] of testConfig("", "") as a tar.gz archive, downloaded with curl
// and verified with sha256sum.
type harnessOptions struct {
	// Config is the tool the script is rendered for; a Config with no Name is testConfig("", "").
	Config update.Config
	// Tag is the tag of the one release the server publishes; "" is harnessTag.
	Tag string
	// Downloader is the one of curl and wget on PATH; "" is curl and [noTool] puts neither.
	Downloader string
	// Hasher is the one of sha256sum and shasum on PATH; "" is sha256sum and [noTool] puts
	// neither.
	Hasher string
	// ServerOptions are the faults the release server is started with.
	ServerOptions []updatetest.Option
	// CompletionFails makes the fake tool's `completion` command print to stderr and exit 3.
	CompletionFails bool
	// NonRoot skips the test when it runs as root, for whom every directory is writable.
	NonRoot bool
}

// harness is one run of the install script against a fake release server, with an environment
// made only of directories under the test's temporary directory.
type harness struct {
	t *testing.T
	// Config is the resolved Config the script is rendered from.
	Config update.Config
	// Release is what Server publishes; its ArchiveName is the asset the script downloads.
	Release updatetest.Release
	// Server is the fake GitHub the script's base URL points at.
	Server *updatetest.Server
	// Script is the rendered install script.
	Script string
	// InstallDir is the directory the script is told to install into; it does not exist before
	// the script runs.
	InstallDir string
	// PathDir holds a symlink to each command the script may run, and nothing else.
	PathDir string
	// TmpDir is the script's TMPDIR, empty before it runs.
	TmpDir string
	// Env is the script's whole environment. A test edits it before Run; deleting a key unsets
	// the variable.
	Env map[string]string
}

// runResult is what one run of the install script printed and how it exited.
type runResult struct {
	Stdout, Stderr string
	Code           int
}

// newHarness renders the script for opts.Config, starts the server publishing the fake tool, and
// builds the environment: HOME, XDG_DATA_HOME, XDG_CONFIG_HOME, TMPDIR and the install directory
// under the test's temporary directory, SHELL naming bash, the base URL naming the server, PATH
// holding PathDir alone, and no version or completion override. It skips the test when the host is
// one the script does not install on or a command the script needs is missing.
func newHarness(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	if opts.NonRoot && os.Geteuid() == 0 {
		t.Skip("every directory is writable for root")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("the install script does not run on %s", runtime.GOOS)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("the install script does not run on %s", runtime.GOARCH)
	}
	if opts.Config.Name == "" {
		opts.Config = testConfig("", "")
	}
	if opts.Tag == "" {
		opts.Tag = harnessTag
	}
	if opts.Downloader == "" {
		opts.Downloader = "curl"
	}
	if opts.Hasher == "" {
		opts.Hasher = "sha256sum"
	}

	cfg, err := opts.Config.Resolved()
	if err != nil {
		t.Fatalf("Resolved: %v", err)
	}
	root := t.TempDir()
	h := &harness{
		t:          t,
		Config:     cfg,
		Script:     render(t, opts.Config),
		InstallDir: filepath.Join(root, "install", "bin"),
		PathDir:    mkdir(t, root, "path"),
		TmpDir:     mkdir(t, root, "tmp"),
	}
	tools := scriptTools()
	for _, tool := range []string{opts.Downloader, opts.Hasher} {
		if tool != noTool {
			tools = append(tools, tool)
		}
	}
	linkTools(t, h.PathDir, tools)
	h.Release = fakeRelease(t, cfg, opts.Tag, opts.CompletionFails)
	h.Server = updatetest.NewServer(t, h.Release, opts.ServerOptions...)

	h.Env = map[string]string{
		"PATH":            h.PathDir,
		"HOME":            mkdir(t, root, "home"),
		"XDG_DATA_HOME":   filepath.Join(root, "xdg-data"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg-config"),
		"TMPDIR":          h.TmpDir,
		"SHELL":           filepath.Join(root, "shells", "bash"),
	}
	h.Env[h.envVar("BASE_URL")] = h.Server.WebURL()
	h.Env[h.envVar("INSTALL_DIR")] = h.InstallDir
	return h
}

// envVar returns the name of the variable the script reads for suffix.
func (h *harness) envVar(suffix string) string {
	h.t.Helper()
	name, err := h.Config.EnvVar(suffix)
	if err != nil {
		h.t.Fatalf("EnvVar(%q): %v", suffix, err)
	}
	return name
}

// BinaryPath returns where the script places the binary.
func (h *harness) BinaryPath() string {
	return filepath.Join(h.InstallDir, h.Config.BinaryName)
}

// CompletionPath returns the file [completioninstall.Target] selects for shell in the harness's
// environment as it stands, or "" when it selects none.
func (h *harness) CompletionPath(shell completioninstall.Shell) string {
	loc, _, ok := completioninstall.Target(shell, h.Config.BinaryName, completioninstall.Env{
		GOOS:          runtime.GOOS,
		Home:          h.Env["HOME"],
		XDGDataHome:   h.Env["XDG_DATA_HOME"],
		XDGConfigHome: h.Env["XDG_CONFIG_HOME"],
	})
	if !ok {
		return ""
	}
	return filepath.FromSlash(loc.Path)
}

// Run runs the script under sh with exactly Env, failing the test when sh cannot be started or the
// script outlives scriptTimeout.
func (h *harness) Run() runResult {
	h.t.Helper()
	script := filepath.Join(h.t.TempDir(), "install.sh")
	if err := os.WriteFile(script, []byte(h.Script), 0o644); err != nil {
		h.t.Fatalf("writing the script: %v", err)
	}
	ctx, cancel := context.WithTimeout(h.t.Context(), scriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shPath(h.t), script)
	cmd.Dir = h.TmpDir
	cmd.Env = make([]string, 0, len(h.Env))
	for k, v := range h.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
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

// fakeToolVersion is what the fake tool tagged tag prints for --version.
func fakeToolVersion(tag string) string {
	return "fake-tool " + tag + "\n"
}

// fakeCompletion is what the fake tool prints for `completion <shell>`.
func fakeCompletion(shell completioninstall.Shell) string {
	return "# fake-tool completion for " + string(shell) + "\n"
}

// fakeTool returns the sh script standing in for the tool tagged tag. It answers --version with
// fakeToolVersion and `completion <shell>` with fakeCompletion, or, when completionFails, with an
// error and exit status 3.
func fakeTool(tag string, completionFails bool) []byte {
	completion := `printf '# fake-tool completion for %s\n' "${2:-}"`
	if completionFails {
		completion = `printf 'fake-tool: no completion command\n' >&2; exit 3`
	}
	return []byte(`#!/bin/sh
case ${1:-} in
--version) printf '%s\n' 'fake-tool ` + tag + `' ;;
completion) ` + completion + ` ;;
*) printf 'fake-tool: unknown command %s\n' "${1:-}" >&2; exit 2 ;;
esac
`)
}

// fakeRelease returns the release tagged tag of the tool cfg describes for the running platform:
// its asset named by cfg's AssetTemplate, a tar.gz archive holding the fake tool as an executable
// entry named BinaryName when the template holds {ext}, and the fake tool itself when it does not.
func fakeRelease(t *testing.T, cfg update.Config, tag string, completionFails bool) updatetest.Release {
	t.Helper()
	fields := asset.Fields{Name: cfg.BinaryName, Tag: tag, OS: runtime.GOOS, Arch: runtime.GOARCH, Ext: cfg.AssetExt}
	assetName, err := asset.Render(cfg.AssetTemplate, fields)
	if err != nil {
		t.Fatalf("rendering the asset name: %v", err)
	}
	checksumsName, err := asset.Render(cfg.ChecksumsAsset, fields)
	if err != nil {
		t.Fatalf("rendering the checksums name: %v", err)
	}
	body := fakeTool(tag, completionFails)
	if asset.UsesExt(cfg.AssetTemplate) {
		// tgz and tar.gz name the same format, which BuildArchive knows by ExtTarGz alone.
		body, err = updatetest.BuildArchive(updatetest.ExtTarGz, []updatetest.Entry{
			{Name: cfg.BinaryName, Mode: 0o755, Body: body},
		})
		if err != nil {
			t.Fatalf("building the archive: %v", err)
		}
	}
	return updatetest.Release{
		Owner:         cfg.Owner,
		Repo:          cfg.Repo,
		Tag:           tag,
		ArchiveName:   assetName,
		Archive:       body,
		ChecksumsName: checksumsName,
	}
}

// linkTools fills dir with a symlink to each of tools, as found on the test's PATH, skipping the
// test when one is missing.
func linkTools(t *testing.T, dir string, tools []string) {
	t.Helper()
	slices.Sort(tools)
	for _, tool := range slices.Compact(tools) {
		target, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
		if target, err = filepath.Abs(target); err != nil {
			t.Fatalf("resolving %s: %v", tool, err)
		}
		if err := os.Symlink(target, filepath.Join(dir, tool)); err != nil {
			t.Fatalf("linking %s: %v", tool, err)
		}
	}
}

// mkdir creates the directory name under parent and returns its path.
func mkdir(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	return dir
}

// dirEntries returns the names in dir, failing the test when it cannot be read.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

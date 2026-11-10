package installscript_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
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
	"go.vipengele.org/update/internal/minisigntest"
	"go.vipengele.org/update/updatetest"
)

// harnessTag is the tag of the release a harness publishes unless its options name another.
const harnessTag = "v1.2.3"

// scriptTimeout bounds one run of the install script, so a script waiting on a server that never
// answers fails the test instead of hanging it.
const scriptTimeout = 2 * time.Minute

// noTool, as a harness's Downloader or Hasher, leaves every candidate for that role off PathDir.
const noTool = "none"

// fakeMinisignEnv names the variable through which TestMain hands the tests the path of the
// minisign stand-in it builds from testdata/fakeminisign.
const fakeMinisignEnv = "INSTALLSCRIPT_TEST_FAKE_MINISIGN"

// fakeToolEnv names the variable through which TestMain hands the Windows tests the path of the
// fake tool it builds from fakeToolSource.
const fakeToolEnv = "INSTALLSCRIPT_TEST_FAKE_TOOL"

// fakeToolSource is the Go program standing in for the tool on Windows, where the sh script
// fakeTool returns cannot run. It answers as fakeTool does, with the tag main.tag is linked with.
const fakeToolSource = `package main

import (
	"fmt"
	"os"
)

var tag = "unset"

func main() {
	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	switch arg {
	case "--version":
		fmt.Printf("fake-tool %s\n", tag)
	case "completion":
		shell := ""
		if len(os.Args) > 2 {
			shell = os.Args[2]
		}
		fmt.Printf("# fake-tool completion for %s\n", shell)
	default:
		fmt.Fprintf(os.Stderr, "fake-tool: unknown command %s\n", arg)
		os.Exit(2)
	}
}
`

// ttyDrainTimeout bounds the wait, once the script has exited, for the last of its terminal output.
// A process the script left running with the terminal open would otherwise hold Run until it exits.
const ttyDrainTimeout = 5 * time.Second

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

// runTests builds the minisign stand-in into a temporary directory, names it in fakeMinisignEnv
// and, on Windows, builds the fake tool into a directory below it and names it in fakeToolEnv;
// then it runs the tests. A stand-in that does not build fails the whole package rather than
// skipping the tests that run the script.
func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "fakeminisign")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating the stand-in's directory: %v\n", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "removing the stand-in: %v\n", err)
		}
	}()
	name := "minisign"
	if runtime.GOOS == "windows" {
		// exec finds a program on Windows only by an extension PATHEXT lists.
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", bin, "./testdata/fakeminisign")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the minisign stand-in: %v\n%s", err, out)
		return 1
	}
	if err := os.Setenv(fakeMinisignEnv, bin); err != nil {
		fmt.Fprintf(os.Stderr, "naming the stand-in: %v\n", err)
		return 1
	}
	if runtime.GOOS == "windows" {
		tool, err := buildFakeTool(filepath.Join(dir, "tool"), harnessTag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "building the fake tool: %v\n", err)
			return 1
		}
		if err := os.Setenv(fakeToolEnv, tool); err != nil {
			fmt.Fprintf(os.Stderr, "naming the fake tool: %v\n", err)
			return 1
		}
	}
	return m.Run()
}

// buildFakeTool builds fakeToolSource, linked with tag, as a module of its own in dir, which it
// creates, and returns the path of the executable. The module stands outside this one, so the
// build reads no go.mod or go.work of the repository.
func buildFakeTool(dir, tag string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for name, body := range map[string]string{
		"go.mod":  "module faketool\n\ngo 1.21\n",
		"main.go": fakeToolSource,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	bin := filepath.Join(dir, "fake-tool")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-ldflags", "-X main.tag="+tag, "-o", bin, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	return bin, nil
}

// fakeMinisign returns the path of the minisign stand-in TestMain built.
func fakeMinisign(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(fakeMinisignEnv)
	if bin == "" {
		t.Fatalf("%s is unset: TestMain did not build the minisign stand-in", fakeMinisignEnv)
	}
	return bin
}

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
// zero value publishes [harnessTag] of testConfig("", "") as a tar.gz archive, downloaded with curl,
// checksummed with sha256sum, with the minisign stand-in on PATH, and run with no terminal. The
// script is rendered before the server exists, so it trusts testConfig's fixed key, not the
// server's; newSignedHarness renders one whose signature check passes against the server.
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
	// NoMinisign leaves minisign off PathDir. By default PathDir holds, as minisign, the stand-in
	// TestMain builds from testdata/fakeminisign, never a minisign found on the test's PATH, so
	// no test depends on the host having one.
	NoMinisign bool
	// TTY runs the script with a pseudo-terminal as its controlling terminal, as `curl | sh` typed
	// into a terminal does: the terminal is the script's /dev/tty and its stderr, and its stdin
	// is not a terminal. Without TTY the script runs in a session of its own on linux, with no
	// controlling terminal, so opening /dev/tty fails. A test with TTY skips on a host other than
	// linux.
	TTY bool
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
	// TTYInput is what Run types into the terminal as the script starts, under the TTY option.
	// The terminal is in canonical mode, so a read sees it a line at a time, and it echoes.
	TTYInput string
	// TTYEOF makes Run type the terminal's end-of-file character after TTYInput, under the TTY
	// option. It ends a read only at the start of a line, so TTYInput is empty or ends in "\n";
	// a read of /dev/tty then returns end of file. Closing the master instead would hang up the
	// terminal.
	TTYEOF bool

	tty bool
}

// runResult is what one run of the install script printed and how it exited.
type runResult struct {
	// Stderr is empty under the TTY option, where the script's stderr is the terminal.
	Stdout, Stderr string
	// TTY is everything written to the terminal, the echo of TTYInput included, with the
	// terminal's "\r\n" line endings.
	TTY  string
	Code int
}

// newHarness renders the script for opts.Config, starts the server publishing the fake tool, and
// builds the environment: HOME, XDG_DATA_HOME, XDG_CONFIG_HOME, TMPDIR and the install directory
// under the test's temporary directory, SHELL naming bash, the base URL naming the server, PATH
// holding PathDir alone, and no version or completion override. PathDir holds the minisign
// stand-in unless opts.NoMinisign. The script is rendered before the server starts, so it does not
// hold the server's key; a test that needs a signature to verify uses newSignedHarness. It skips
// the test when the host is one the script does not install on or a command the script needs is
// missing.
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
		tty:        opts.TTY,
	}
	tools := scriptTools()
	for _, tool := range []string{opts.Downloader, opts.Hasher} {
		if tool != noTool {
			tools = append(tools, tool)
		}
	}
	linkTools(t, h.PathDir, tools)
	if !opts.NoMinisign {
		if err := os.Symlink(fakeMinisign(t), filepath.Join(h.PathDir, "minisign")); err != nil {
			t.Fatalf("linking minisign: %v", err)
		}
	}
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

// Run runs the script under sh with exactly Env and, under the TTY option, with the terminal
// attached, failing the test when sh cannot be started or the script outlives scriptTimeout.
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
	var term *terminal
	if h.tty {
		term = h.attachTerminal(cmd)
	} else {
		detachTerminal(cmd)
	}
	var ttyOut string
	err := cmd.Start()
	if err == nil {
		if term != nil {
			term.start()
		}
		err = cmd.Wait()
		if term != nil {
			ttyOut = term.finish()
		}
	}
	if ctx.Err() != nil {
		h.t.Fatalf("the script did not finish within %v\nstdout:\n%s\nstderr:\n%s\nterminal:\n%s", scriptTimeout, stdout.String(), stderr.String(), ttyOut)
	}
	res := runResult{Stdout: stdout.String(), Stderr: stderr.String(), TTY: ttyOut}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		res.Code = exitErr.ExitCode()
	case err != nil:
		h.t.Fatalf("running the script: %v", err)
	}
	return res
}

// terminal is the pseudo-terminal of one run of the script.
type terminal struct {
	t             *testing.T
	master, slave *os.File
	input         []byte
	out           bytes.Buffer
	done          chan struct{}
}

// attachTerminal attaches a pseudo-terminal to cmd, skipping the test where the harness has none,
// and prepares TTYInput and TTYEOF to be typed into it.
func (h *harness) attachTerminal(cmd *exec.Cmd) *terminal {
	h.t.Helper()
	master, slave, eof := attachPTY(h.t, cmd)
	input := []byte(h.TTYInput)
	if h.TTYEOF {
		input = append(input, eof)
	}
	return &terminal{t: h.t, master: master, slave: slave, input: input, done: make(chan struct{})}
}

// start runs once the script has started: it closes the harness's copy of the slave, so a read of
// the master ends when the script's last copy closes, types the input and starts collecting the
// output.
func (term *terminal) start() {
	term.t.Helper()
	if err := term.slave.Close(); err != nil {
		term.t.Fatalf("closing the pty slave: %v", err)
	}
	if _, err := term.master.Write(term.input); err != nil {
		term.t.Fatalf("typing into the terminal: %v", err)
	}
	go func() {
		defer close(term.done)
		// The read ends with EIO once no process holds the slave open.
		_, _ = io.Copy(&term.out, term.master)
	}()
}

// finish returns what the script wrote to the terminal, waiting at most ttyDrainTimeout for a
// process that still holds the terminal open.
func (term *terminal) finish() string {
	term.t.Helper()
	select {
	case <-term.done:
	default:
		if err := term.master.SetReadDeadline(time.Now().Add(ttyDrainTimeout)); err != nil {
			term.t.Fatalf("bounding the terminal's last read: %v", err)
		}
		<-term.done
	}
	return term.out.String()
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

// signedFile writes msg to a file in a new temporary directory and the signature file sig beside
// it, where minisign looks for it by default, and returns the message file's path.
func signedFile(t *testing.T, msg, sig []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(path, msg, 0o644); err != nil {
		t.Fatalf("writing the message: %v", err)
	}
	if err := os.WriteFile(path+".minisig", sig, 0o644); err != nil {
		t.Fatalf("writing the signature: %v", err)
	}
	return path
}

// generateKey returns a new minisign key, failing the test on an error.
func generateKey(t *testing.T) *minisigntest.Key {
	t.Helper()
	key, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

func TestFakeMinisignVerifies(t *testing.T) {
	t.Parallel()
	const comment = "timestamp:1 file:checksums.txt"
	msg := []byte("0123  fake-tool.tar.gz\n")
	key, other := generateKey(t), generateKey(t)

	tests := []struct {
		name string
		// args builds the arguments from the message file's path.
		args       func(file string) []string
		sig        []byte
		tamper     bool
		wantCode   int
		wantStdout string
	}{
		{
			name:       "valid signature beside the file",
			args:       func(file string) []string { return []string{"-V", "-P", key.PublicKeyLine(), "-m", file} },
			sig:        key.Sign(msg, comment),
			wantStdout: "Signature and comment signature verified\nTrusted comment: " + comment + "\n",
		},
		{
			name: "valid signature, options reordered, quiet",
			args: func(file string) []string {
				return []string{"-q", "-m", file, "-x", file + ".minisig", "-P", key.PublicKeyLine(), "-V"}
			},
			sig: key.Sign(msg, comment),
		},
		{
			name:       "valid signature, bundled options, trusted comment only",
			args:       func(file string) []string { return []string{"-VQ", "-P" + key.PublicKeyLine(), "-m" + file} },
			sig:        key.Sign(msg, comment),
			wantStdout: comment + "\n",
		},
		{
			name:     "tampered message",
			args:     func(file string) []string { return []string{"-V", "-P", key.PublicKeyLine(), "-m", file} },
			sig:      key.Sign(msg, comment),
			tamper:   true,
			wantCode: 1,
		},
		{
			name:     "wrong key",
			args:     func(file string) []string { return []string{"-V", "-P", other.PublicKeyLine(), "-m", file} },
			sig:      key.Sign(msg, comment),
			wantCode: 1,
		},
		{
			name:     "legacy signature when prehashed is required",
			args:     func(file string) []string { return []string{"-V", "-H", "-P", key.PublicKeyLine(), "-m", file} },
			sig:      key.SignLegacy(msg, comment),
			wantCode: 1,
		},
		{
			name: "missing signature file",
			args: func(file string) []string {
				return []string{"-V", "-P", key.PublicKeyLine(), "-m", file, "-x", file + ".absent"}
			},
			sig:      key.Sign(msg, comment),
			wantCode: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := signedFile(t, msg, tt.sig)
			if tt.tamper {
				if err := os.WriteFile(file, append(slices.Clone(msg), '\n'), 0o644); err != nil {
					t.Fatalf("tampering with the message: %v", err)
				}
			}
			cmd := exec.CommandContext(t.Context(), fakeMinisign(t), tt.args(file)...)
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			var exitErr *exec.ExitError
			switch {
			case errors.As(err, &exitErr):
				code = exitErr.ExitCode()
			case err != nil:
				t.Fatalf("running the stand-in: %v", err)
			}
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, tt.wantCode, stderr.String())
			}
			if stdout.String() != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
		})
	}
}

func TestHarnessMinisign(t *testing.T) {
	t.Parallel()
	t.Run("on PATH by default", func(t *testing.T) {
		t.Parallel()
		key := generateKey(t)
		msg := []byte("signed\n")
		file := signedFile(t, msg, key.Sign(msg, "comment"))
		h := newHarness(t, harnessOptions{})
		h.Script = "command -v minisign\nminisign -V -q -P '" + key.PublicKeyLine() + "' -m '" + file + "'\n"
		res := h.Run()
		if res.Code != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr:\n%s", res.Code, res.Stderr)
		}
		if want := filepath.Join(h.PathDir, "minisign") + "\n"; res.Stdout != want {
			t.Fatalf("stdout = %q, want %q", res.Stdout, want)
		}
	})
	t.Run("off PATH with NoMinisign", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, harnessOptions{NoMinisign: true})
		h.Script = "command -v minisign\n"
		if res := h.Run(); res.Code == 0 {
			t.Fatalf("command -v minisign succeeded, printing %q", res.Stdout)
		}
	})
}

func TestHarnessTTY(t *testing.T) {
	t.Parallel()
	const script = `printf 'answer? ' >&2
if read answer </dev/tty; then printf 'read:%s\n' "$answer"; else printf 'eof:%s\n' "$answer"; fi
`
	tests := []struct {
		name       string
		input      string
		eof        bool
		wantStdout string
	}{
		{name: "a line", input: "yes\n", wantStdout: "read:yes\n"},
		{name: "end of file", eof: true, wantStdout: "eof:\n"},
		{name: "end of file after a line", input: "no\n", eof: true, wantStdout: "read:no\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{TTY: true})
			h.Script = script
			h.TTYInput, h.TTYEOF = tt.input, tt.eof
			res := h.Run()
			if res.Code != 0 {
				t.Fatalf("exit code = %d, want 0\nterminal:\n%s", res.Code, res.TTY)
			}
			if res.Stdout != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q\nterminal:\n%q", res.Stdout, tt.wantStdout, res.TTY)
			}
			if !strings.Contains(res.TTY, "answer? ") {
				t.Fatalf("terminal = %q, want the prompt written to stderr", res.TTY)
			}
			if res.Stderr != "" {
				t.Fatalf("stderr = %q, want it empty: stderr is the terminal", res.Stderr)
			}
		})
	}
	t.Run("no terminal", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, harnessOptions{})
		h.Script = script
		res := h.Run()
		if strings.Contains(res.Stdout, "read:") {
			t.Fatalf("stdout = %q: the script read a line with no terminal", res.Stdout)
		}
		if res.TTY != "" {
			t.Fatalf("terminal = %q, want nothing without the TTY option", res.TTY)
		}
	})
}

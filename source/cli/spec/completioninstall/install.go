package completioninstall

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"go.vipengele.org/cli/spec"
)

// NoCompletionFlag is the name of the flag that turns the installation off. The consumer
// registers it on its own command, typically `update`, and passes its value as
// [Options.Disabled].
const NoCompletionFlag = "no-completion"

// NoCompletionEnvSuffix is the suffix of the environment variable that turns the installation
// off: [spec.Identity.EnvVar] of it names the variable, as in ATK_NO_COMPLETION for the prefix
// ATK.
const NoCompletionEnvSuffix = "NO_COMPLETION"

// Options says which tool's completion script [Install] installs, and for which shell. Every
// seam is optional: a nil function and an empty GOOS use the real process, so the zero value of
// every field but Identity installs for the running user.
type Options struct {
	// Identity is the tool's identity. Its Name names the completion file, and its EnvVar of
	// [NoCompletionEnvSuffix] names the variable that turns the installation off. Taking the
	// identity rather than a name and a variable keeps the file name and the opt-out variable
	// those of the tool the user typed, with no prefix spelled out in this package.
	Identity spec.Identity
	// Shell is the shell to install for. "" detects it from $SHELL with [ParseShell]: an unset
	// or empty $SHELL, as on windows outside a Unix-like shell, is [ReasonShellNotDetected], and
	// a $SHELL naming any other program is [ReasonUnsupportedShell].
	Shell Shell
	// Disabled carries the consumer's [NoCompletionFlag]. When true, Install does nothing.
	Disabled bool
	// ExecutablePath is the binary whose `completion <shell>` command prints the script. ""
	// means the binary the Executable seam returns.
	ExecutablePath string

	// GOOS selects the target table, as a value of runtime.GOOS. "" means runtime.GOOS.
	GOOS string
	// LookupEnv returns an environment variable's value and whether it is set, as os.LookupEnv
	// does. Nil means os.LookupEnv. A variable set to "" counts as unset.
	LookupEnv func(key string) (string, bool)
	// UserHomeDir returns the user's home directory, as os.UserHomeDir does. Nil means
	// os.UserHomeDir. A failure or "" leaves the home directory absent.
	UserHomeDir func() (string, error)
	// BrewPrefix returns the Homebrew prefix. Nil runs `brew --prefix`. It is called only for
	// zsh, off windows, when HOMEBREW_PREFIX is unset; a failure or "" leaves the prefix absent.
	BrewPrefix func() (string, error)
	// Run runs exe with args and returns what it prints on standard output. Nil runs the
	// process with os/exec; a non-zero exit is an error.
	Run func(exe string, args ...string) ([]byte, error)
	// WriteFile writes data to the file path with the permission bits perm. Nil means the
	// package's atomic writer, which creates the missing parent directories at 0755, writes a
	// temporary file beside path and renames it over path, leaving no temporary file behind on
	// failure.
	WriteFile func(path string, data []byte, perm fs.FileMode) error
	// Executable returns the path of the running binary, as os.Executable does. Nil means
	// os.Executable. It is called only when ExecutablePath is "".
	Executable func() (string, error)

	// readFile reads the existing target. Nil means os.ReadFile.
	readFile func(path string) ([]byte, error)
	// dirWritable reports that the directory exists and a file can be created in it. Nil means
	// dirWritable.
	dirWritable func(dir string) bool
}

// Result is what [Install] did.
type Result struct {
	// Shell is the shell installed for, or detected; "" when no shell was detected.
	Shell Shell
	// Path is the file the script is written to, with the host's separators.
	Path string
	// Note is what the user has to do for the shell to load the file, and "" when it loads it
	// as written.
	Note string
	// Skipped reports that nothing is installed, and Reason says why.
	Skipped bool
	// Reason is why the installation was skipped, and "" when it was not.
	Reason Reason
	// Changed reports that the file was written: false when it already held the script.
	Changed bool
}

// Install writes the completion script of the tool opts.Identity names for the user's shell to
// the path [Target] chooses.
//
// It skips, with a nil error and a Result whose Skipped is true, when opts.Disabled is true or
// the opt-out variable is "1" ([ReasonDisabled]; no other value disables), and whenever Target
// answers no path. Otherwise it runs `<exe> completion <shell>` and writes what the process
// prints, unless the file already holds exactly that, in which case Changed is false and nothing
// is written. An existing file that cannot be read is written over.
//
// Install returns an error for an invalid opts.Identity, for failing to locate the executable,
// for failing to generate the script — which is what a tool built without
// core.WithCompletion() answers, since its `completion` command does not exist — and for
// failing to write the file. A script that cannot be generated leaves the file untouched.
func Install(opts Options) (Result, error) {
	if opts.Disabled {
		return Result{Skipped: true, Reason: ReasonDisabled}, nil
	}
	if err := opts.Identity.Validate(); err != nil {
		return Result{}, fmt.Errorf("completioninstall: invalid identity: %w", err)
	}
	s := newSeams(opts)
	// EnvVar cannot fail once Validate passes: the prefix is a valid identifier of at most
	// spec.MaxEnvPrefixLen bytes, NoCompletionEnvSuffix is one, and the joined name stays within
	// the length an environment variable allows.
	envVar, _ := opts.Identity.EnvVar(NoCompletionEnvSuffix)
	if s.env(envVar) == "1" {
		return Result{Skipped: true, Reason: ReasonDisabled}, nil
	}

	shell := opts.Shell
	if shell == "" {
		program := s.env("SHELL")
		if program == "" {
			return Result{Skipped: true, Reason: ReasonShellNotDetected}, nil
		}
		var ok bool
		if shell, ok = ParseShell(program); !ok {
			return Result{Skipped: true, Reason: ReasonUnsupportedShell}, nil
		}
	}

	loc, reason, ok := Target(shell, opts.Identity.Name, s.targetEnv(shell))
	if !ok {
		return Result{Shell: shell, Skipped: true, Reason: reason}, nil
	}
	res := Result{Shell: shell, Path: filepath.FromSlash(loc.Path), Note: loc.Note}

	exe := opts.ExecutablePath
	if exe == "" {
		var err error
		if exe, err = s.executable(); err != nil {
			return res, fmt.Errorf("completioninstall: locating the executable: %w", err)
		}
	}
	script, err := s.run(exe, "completion", string(shell))
	if err != nil {
		return res, fmt.Errorf("completioninstall: generating the %s completion script with %s "+
			"(completion must be enabled, with core.WithCompletion()): %w", shell, exe, err)
	}

	if existing, err := s.readFile(res.Path); err == nil && bytes.Equal(existing, script) {
		return res, nil
	}
	if err := s.writeFile(res.Path, script, 0o644); err != nil {
		return res, fmt.Errorf("completioninstall: writing %s: %w", res.Path, err)
	}
	res.Changed = true
	return res, nil
}

// seams are the options' functions with every nil replaced by the real process.
type seams struct {
	goos        string
	lookupEnv   func(string) (string, bool)
	userHomeDir func() (string, error)
	brewPrefix  func() (string, error)
	run         func(string, ...string) ([]byte, error)
	writeFile   func(string, []byte, fs.FileMode) error
	executable  func() (string, error)
	readFile    func(string) ([]byte, error)
	dirWritable func(string) bool
}

func newSeams(opts Options) seams {
	s := seams{
		goos:        opts.GOOS,
		lookupEnv:   opts.LookupEnv,
		userHomeDir: opts.UserHomeDir,
		brewPrefix:  opts.BrewPrefix,
		run:         opts.Run,
		writeFile:   opts.WriteFile,
		executable:  opts.Executable,
		readFile:    opts.readFile,
		dirWritable: opts.dirWritable,
	}
	if s.goos == "" {
		s.goos = runtime.GOOS
	}
	if s.lookupEnv == nil {
		s.lookupEnv = os.LookupEnv
	}
	if s.userHomeDir == nil {
		s.userHomeDir = os.UserHomeDir
	}
	if s.brewPrefix == nil {
		s.brewPrefix = brewPrefix
	}
	if s.run == nil {
		s.run = run
	}
	if s.writeFile == nil {
		s.writeFile = writeFileAtomic
	}
	if s.executable == nil {
		s.executable = os.Executable
	}
	if s.readFile == nil {
		s.readFile = os.ReadFile
	}
	if s.dirWritable == nil {
		s.dirWritable = dirWritable
	}
	return s
}

// env returns the value of the variable key, and "" when it is unset or set to "".
func (s seams) env(key string) string {
	v, _ := s.lookupEnv(key)
	return v
}

// targetEnv resolves the facts [Target] reads for shell, in forward-slash form. Homebrew is
// looked up, and its zsh directory probed, only for zsh off windows: no other target reads them.
func (s seams) targetEnv(shell Shell) Env {
	env := Env{
		GOOS:          s.goos,
		XDGDataHome:   filepath.ToSlash(s.env("XDG_DATA_HOME")),
		XDGConfigHome: filepath.ToSlash(s.env("XDG_CONFIG_HOME")),
	}
	if home, err := s.userHomeDir(); err == nil {
		env.Home = filepath.ToSlash(home)
	}
	if shell != Zsh || s.goos == "windows" {
		return env
	}
	prefix := s.env("HOMEBREW_PREFIX")
	if prefix == "" {
		if p, err := s.brewPrefix(); err == nil {
			prefix = strings.TrimSpace(p)
		}
	}
	if prefix != "" {
		env.BrewPrefix = filepath.ToSlash(prefix)
		env.BrewDirUsable = s.dirWritable(filepath.FromSlash(ZshBrewDir(env.BrewPrefix)))
	}
	return env
}

// brewPrefix runs `brew --prefix` and returns what it prints, trimmed.
func brewPrefix() (string, error) {
	out, err := run("brew", "--prefix")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// run runs exe with args and returns its standard output. A non-zero exit is an error carrying
// what the process printed on standard error.
func run(exe string, args ...string) ([]byte, error) {
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- exe is the embedding tool's own binary, given by the program as Options.ExecutablePath or os.Executable, or the fixed name brew; it runs without a shell, with the fixed argument completion and a Shell constant, or --prefix
	out, err := exec.Command(exe, args...).Output() // #nosec G204 -- exe is the embedding tool's own binary, given by the program as Options.ExecutablePath or os.Executable, or the fixed name brew; it runs without a shell, with the fixed argument completion and a Shell constant, or --prefix; running it is the purpose of Install
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return nil, fmt.Errorf("%w: %s", err, stderr)
		}
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// dirWritable reports that dir exists and a file can be created in it, by creating a temporary
// file there. The file is removed before dirWritable returns.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".completioninstall-probe-*")
	if err != nil {
		return false
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_ = f.Close()
	return true
}

// writeFileAtomic writes data to path with the permission bits perm through the real file
// system; see [atomicSteps.write].
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	return realSteps().write(path, data, perm)
}

// atomicSteps are the file-system operations of an atomic write, one field each.
type atomicSteps struct {
	mkdirAll   func(path string, perm fs.FileMode) error
	createTemp func(dir, pattern string) (*os.File, error)
	rename     func(oldpath, newpath string) error
}

func realSteps() atomicSteps {
	return atomicSteps{mkdirAll: os.MkdirAll, createTemp: os.CreateTemp, rename: os.Rename}
}

// write creates path's directory and its missing parents at 0755, writes data to a temporary
// file in that directory, sets it to perm and renames it over path. A reader of path sees the
// old content or the new, never part of either. The temporary file is removed on every failure;
// after a successful rename it no longer exists.
func (s atomicSteps) write(path string, data []byte, perm fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := s.mkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := s.createTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()

	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Chmod(perm), f.Sync(), f.Close()); err != nil {
		return err
	}
	return s.rename(tmp, path)
}

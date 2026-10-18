package config_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"

	"go.vipengele.org/cli/core/command/config"
	"go.vipengele.org/cli/spec"
	cfg "go.vipengele.org/config"
)

const (
	validEdit   = "name: edited\nport: 9090\n"
	invalidEdit = "name: edited\nbogus: 1\n"
)

// stdinTerminal reports the standard input, and only it, as a terminal.
func stdinTerminal(s spec.Stream) bool { return s == spec.Stdin }

// fakeEditor is an Editor that writes the next of contents to the file on each call and records
// what the file held when it was opened. It fails t when called more often than contents allows.
type fakeEditor struct {
	t        *testing.T
	contents []string
	seen     []string
}

func (f *fakeEditor) edit(_ context.Context, _ *spec.Env, path string) error {
	f.t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatalf("editor read %s: %v", path, err)
	}
	f.seen = append(f.seen, string(before))
	if len(f.seen) > len(f.contents) {
		f.t.Fatalf("editor opened %d times, want at most %d", len(f.seen), len(f.contents))
	}
	if err := os.WriteFile(path, []byte(f.contents[len(f.seen)-1]), 0o600); err != nil {
		f.t.Fatalf("editor write %s: %v", path, err)
	}
	return nil
}

// runEdit runs "config edit" for opts against env, with colour off, and returns what it wrote.
func runEdit(t *testing.T, opts config.Options[testConfig], env *spec.Env) result {
	t.Helper()
	var out, errOut bytes.Buffer
	if env.Stdout == nil {
		env.Stdout = &out
	}
	if env.Stderr == nil {
		env.Stderr = &errOut
	}
	inv := spec.NewInvocation(env, nil, nil)
	inv.Output = spec.NewOutput(spec.OutputOptions{NoColorStdout: true, NoColorStderr: true})
	err := subcommand(t, config.New(opts), "edit").Run(context.Background(), inv)
	return result{stdout: out.String(), stderr: errOut.String(), err: err}
}

// editWith returns the Options of a file holding content, or no file when content is "", edited
// by an editor writing contents in turn.
func editWith(t *testing.T, content string, contents ...string) (config.Options[testConfig], *fakeEditor) {
	t.Helper()
	opts := options(t, content)
	fake := &fakeEditor{t: t, contents: contents}
	opts.Editor = fake.edit
	return opts, fake
}

// terminalEnv returns an Env whose standard input, reading stdin, is a terminal.
func terminalEnv(stdin string) *spec.Env {
	return &spec.Env{Stdin: strings.NewReader(stdin), Terminal: stdinTerminal}
}

// assertFile fails t unless the file at path holds want with mode 0600.
func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s holds %q, want %q", path, got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode of %s = %o, want 600", path, mode)
	}
}

// assertNoTempDir fails t when the directory of path holds anything but the file at path.
func assertNoTempDir(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read directory of %s: %v", path, err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("directory of %s holds %q, want only the configuration file", path, e.Name())
		}
	}
}

func TestEditIsAnnotatedSkipConfigWithoutArgsOrFlags(t *testing.T) {
	edit := subcommand(t, config.New(options(t, "")), "edit")
	if _, ok := edit.Annotations[spec.SkipConfig]; !ok {
		t.Errorf("edit is not annotated %s", spec.SkipConfig)
	}
	if len(edit.Flags) != 0 {
		t.Errorf("edit declares %d flags, want none", len(edit.Flags))
	}
	if err := edit.Args.Check([]string{"extra"}); err == nil {
		t.Error("edit accepts a positional argument, want none")
	}
}

func TestEditWritesAValidChange(t *testing.T) {
	opts, fake := editWith(t, "name: tool\n", validEdit)
	r := runEdit(t, opts, terminalEnv(""))
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	assertFile(t, opts.Path, validEdit)
	assertNoTempDir(t, opts.Path)
	if got := fake.seen[0]; got != "name: tool\n" {
		t.Errorf("editor opened %q, want the file's content", got)
	}
	if want := opts.Path + " updated"; !strings.Contains(r.stderr, want) {
		t.Errorf("stderr = %q, want a line holding %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
}

func TestEditReEditsAnInvalidChangeUntilValid(t *testing.T) {
	for _, answer := range []string{"\n", "y\n", " YES \n", "yes"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			opts, fake := editWith(t, "name: tool\n", invalidEdit, validEdit)
			r := runEdit(t, opts, terminalEnv(answer))
			if r.err != nil {
				t.Fatalf("Run: %v", r.err)
			}
			if len(fake.seen) != 2 || fake.seen[1] != invalidEdit {
				t.Errorf("editor opened %q, want the invalid edit opened again", fake.seen)
			}
			for _, want := range []string{"bogus", "config.yaml:2", "re-edit? [Y/n]"} {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("stderr = %q, want it to mention %q", r.stderr, want)
				}
			}
			assertFile(t, opts.Path, validEdit)
			assertNoTempDir(t, opts.Path)
		})
	}
}

func TestEditDeclinedLeavesTheOriginalUntouched(t *testing.T) {
	for _, tt := range []struct{ name, stdin string }{
		{"no", "n\n"},
		{"NO", "NO\n"},
		{"end of input", ""},
		{"no after an unknown answer", "maybe\nno\n"},
		{"an unknown answer ending the input", "maybe"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const original = "name: tool\nport: 1234\n"
			opts, fake := editWith(t, original, invalidEdit)
			if err := os.Chmod(opts.Path, 0o644); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			r := runEdit(t, opts, terminalEnv(tt.stdin))
			if !errors.Is(r.err, config.ErrEditAbandoned) {
				t.Fatalf("Run = %v, want an error matching ErrEditAbandoned", r.err)
			}
			if !strings.Contains(r.err.Error(), opts.Path+" unchanged") {
				t.Errorf("error %q does not say %s is unchanged", r.err, opts.Path)
			}
			if len(fake.seen) != 1 {
				t.Errorf("editor opened %d times, want once", len(fake.seen))
			}
			got, err := os.ReadFile(opts.Path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(got) != original {
				t.Errorf("file holds %q, want the original %q", got, original)
			}
			if info, _ := os.Stat(opts.Path); info.Mode().Perm() != 0o644 {
				t.Errorf("mode = %o, want the original 644", info.Mode().Perm())
			}
			assertNoTempDir(t, opts.Path)
		})
	}
}

func TestEditReadsEveryAnswerFromOneReader(t *testing.T) {
	opts, fake := editWith(t, "name: tool\n", invalidEdit, "name: edited\nbogus: 2\n")
	r := runEdit(t, opts, terminalEnv("y\nn\n"))
	if !errors.Is(r.err, config.ErrEditAbandoned) {
		t.Fatalf("Run = %v, want the second answer to decline", r.err)
	}
	if len(fake.seen) != 2 {
		t.Errorf("editor opened %d times, want twice", len(fake.seen))
	}
	if got := strings.Count(r.stderr, "re-edit? [Y/n]"); got != 2 {
		t.Errorf("prompted %d times, want 2; stderr %q", got, r.stderr)
	}
}

func TestEditUnchangedWritesNothing(t *testing.T) {
	const original = "name: tool\n"
	opts, _ := editWith(t, original, original)
	if err := os.Chmod(opts.Path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	r := runEdit(t, opts, terminalEnv(""))
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if !strings.Contains(r.stderr, "no changes") {
		t.Errorf("stderr = %q, want it to report no changes", r.stderr)
	}
	if info, _ := os.Stat(opts.Path); info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want the file left as it was at 644", info.Mode().Perm())
	}
	assertNoTempDir(t, opts.Path)
}

func TestEditUnchangedAfterAnInvalidEditWritesNothing(t *testing.T) {
	const original = "name: tool\n"
	opts, _ := editWith(t, original, invalidEdit, original)
	r := runEdit(t, opts, terminalEnv("\n"))
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if !strings.Contains(r.stderr, "no changes") {
		t.Errorf("stderr = %q, want it to report no changes", r.stderr)
	}
}

// seeded returns what cfg.Seed writes for the test's defaults.
func seeded(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := cfg.Seed(path, defaults()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	return string(data)
}

func TestEditSeedsAMissingFile(t *testing.T) {
	opts, fake := editWith(t, "", validEdit)
	r := runEdit(t, opts, terminalEnv(""))
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if got, want := fake.seen[0], seeded(t); got != want {
		t.Errorf("editor opened %q, want the seeded default %q", got, want)
	}
	assertFile(t, opts.Path, validEdit)
	assertNoTempDir(t, opts.Path)
}

func TestEditUnchangedSeedCreatesNoFile(t *testing.T) {
	opts, _ := editWith(t, "", seeded(t))
	r := runEdit(t, opts, terminalEnv(""))
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if !strings.Contains(r.stderr, "no changes") {
		t.Errorf("stderr = %q, want it to report no changes", r.stderr)
	}
	if _, err := os.Stat(opts.Path); !os.IsNotExist(err) {
		t.Errorf("Stat = %v, want no file created", err)
	}
	assertNoTempDir(t, opts.Path)
}

func TestEditCreatesAMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	fake := &fakeEditor{t: t, contents: []string{validEdit}}
	opts := config.Options[testConfig]{Path: filepath.Join(dir, "config.yaml"), Defaults: defaults(), Editor: fake.edit}
	if r := runEdit(t, opts, terminalEnv("")); r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("mode of the created directory = %o, want 700", mode)
	}
	assertFile(t, opts.Path, validEdit)
}

func TestEditRefusesANonTerminal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	called := false
	opts := config.Options[testConfig]{
		Path:     filepath.Join(dir, "config.yaml"),
		Defaults: defaults(),
		Editor:   func(context.Context, *spec.Env, string) error { called = true; return nil },
	}
	r := runEdit(t, opts, &spec.Env{Terminal: func(spec.Stream) bool { return false }})
	if !errors.Is(r.err, config.ErrNoTerminal) {
		t.Fatalf("Run = %v, want an error matching ErrNoTerminal", r.err)
	}
	if called {
		t.Error("the editor ran, want it not to")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat of the config directory = %v, want it not created", err)
	}
}

func TestEditWithoutAnEditorConfigured(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  map[string]string
	}{
		{"neither set", nil},
		{"both blank", map[string]string{"VISUAL": " ", "EDITOR": ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "absent")
			opts := config.Options[testConfig]{Path: filepath.Join(dir, "config.yaml"), Defaults: defaults()}
			env := terminalEnv("")
			env.LookupEnv = func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok }
			r := runEdit(t, opts, env)
			if !errors.Is(r.err, config.ErrNoEditor) {
				t.Fatalf("Run = %v, want an error matching ErrNoEditor", r.err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("Stat of the config directory = %v, want it not created", err)
			}
		})
	}
}

func TestEditEditorErrorLeavesTheOriginal(t *testing.T) {
	const original = "name: tool\n"
	opts := options(t, original)
	failure := errors.New("editor crashed")
	opts.Editor = func(_ context.Context, _ *spec.Env, path string) error {
		_ = os.WriteFile(path, []byte(validEdit), 0o600)
		return failure
	}
	r := runEdit(t, opts, terminalEnv(""))
	if !errors.Is(r.err, failure) {
		t.Fatalf("Run = %v, want the editor's error", r.err)
	}
	got, _ := os.ReadFile(opts.Path)
	if string(got) != original {
		t.Errorf("file holds %q, want the original", got)
	}
	assertNoTempDir(t, opts.Path)
}

func TestEditAcceptsAnEditorThatReplacesTheInode(t *testing.T) {
	opts := options(t, "name: tool\n")
	if err := os.Chmod(opts.Path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	opts.Editor = func(_ context.Context, _ *spec.Env, path string) error {
		next := path + ".new"
		if err := os.WriteFile(next, []byte(validEdit), 0o644); err != nil {
			return err
		}
		if err := os.Chmod(next, 0o644); err != nil {
			return err
		}
		return os.Rename(next, path)
	}
	if r := runEdit(t, opts, terminalEnv("")); r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	assertFile(t, opts.Path, validEdit)
	assertNoTempDir(t, opts.Path)
}

// stdioFiles returns an Env whose standard streams are files: stdin holding nothing, stdout and
// stderr new empty files, the standard input reported as a terminal.
func stdioFiles(t *testing.T) (*spec.Env, *os.File) {
	t.Helper()
	dir := t.TempDir()
	open := func(name string) *os.File {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	stderr := open("stderr")
	return &spec.Env{Stdin: open("stdin"), Stdout: open("stdout"), Stderr: stderr, Terminal: stdinTerminal}, stderr
}

func TestEditRunsVisualBeforeEditor(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	scripts := t.TempDir()
	visual := filepath.Join(scripts, "visual.sh")
	script := "#!/bin/sh\n[ \"$1\" = -w ] || exit 3\nprintf 'name: edited\\nport: 9090\\n' > \"$2\"\n"
	if err := os.WriteFile(visual, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	opts := options(t, "name: tool\n")
	env, stderr := stdioFiles(t)
	vars := map[string]string{"VISUAL": "/bin/sh " + visual + " -w", "EDITOR": "/bin/false"}
	env.LookupEnv = func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
	inv := spec.NewInvocation(env, nil, nil)
	if err := subcommand(t, config.New(opts), "edit").Run(context.Background(), inv); err != nil {
		if _, seekErr := stderr.Seek(0, io.SeekStart); seekErr == nil {
			out, _ := io.ReadAll(stderr)
			t.Logf("stderr: %s", out)
		}
		t.Fatalf("Run: %v", err)
	}
	assertFile(t, opts.Path, validEdit)
	assertNoTempDir(t, opts.Path)
}

func TestEditReportsAFailingEditorProgram(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	const original = "name: tool\n"
	opts := options(t, original)
	env, _ := stdioFiles(t)
	env.LookupEnv = func(k string) (string, bool) { return "/bin/sh -c 'exit 4'", k == "EDITOR" }
	inv := spec.NewInvocation(env, nil, nil)
	if err := subcommand(t, config.New(opts), "edit").Run(context.Background(), inv); err == nil {
		t.Fatal("Run succeeded, want the editor's exit status")
	}
	got, _ := os.ReadFile(opts.Path)
	if string(got) != original {
		t.Errorf("file holds %q, want the original", got)
	}
	assertNoTempDir(t, opts.Path)
}

func TestEditDefaultEditorRefusesStreamsThatAreNotFiles(t *testing.T) {
	const original = "name: tool\n"
	opts := options(t, original)
	marker := filepath.Join(t.TempDir(), "ran")
	env := terminalEnv("")
	env.LookupEnv = func(k string) (string, bool) { return "/usr/bin/touch " + marker, k == "EDITOR" }
	r := runEdit(t, opts, env)
	if !errors.Is(r.err, config.ErrNotFileStdio) {
		t.Fatalf("Run = %v, want an error matching ErrNotFileStdio", r.err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("Stat of the marker = %v, want the editor not run", err)
	}
	got, _ := os.ReadFile(opts.Path)
	if string(got) != original {
		t.Errorf("file holds %q, want the original", got)
	}
	assertNoTempDir(t, opts.Path)
}

// errWriter is a writer whose every write fails with err.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// neverCalled returns an Editor that fails t when it runs.
func neverCalled(t *testing.T) config.Editor {
	t.Helper()
	return func(context.Context, *spec.Env, string) error {
		t.Error("the editor ran, want it not to")
		return nil
	}
}

// assertContent fails t unless the file at path holds want.
func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s holds %q, want %q", path, got, want)
	}
}

// TestEditFailsWhenTheDirectoryCannotBeCreated places the configuration file under a regular
// file, so that its directory cannot be created.
func TestEditFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts := config.Options[testConfig]{
		Path:     filepath.Join(parent, "sub", "config.yaml"),
		Defaults: defaults(),
		Editor:   neverCalled(t),
	}
	r := runEdit(t, opts, terminalEnv(""))
	if r.err == nil {
		t.Fatal("Run succeeded, want the directory error")
	}
	if !strings.HasPrefix(r.err.Error(), "config edit: ") {
		t.Errorf("error %q does not start with %q", r.err, "config edit: ")
	}
	assertContent(t, parent, "x")
}

// TestEditFailsWhenNoTemporaryDirectoryCanBeMade uses /proc/self, a directory that exists and in
// which no directory can be created, whoever runs the test.
func TestEditFailsWhenNoTemporaryDirectoryCanBeMade(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs the Linux /proc filesystem")
	}
	if info, err := os.Stat("/proc/self"); err != nil || !info.IsDir() {
		t.Skip("no /proc/self directory")
	}
	opts := config.Options[testConfig]{
		Path:     "/proc/self/config.yaml",
		Defaults: defaults(),
		Editor:   neverCalled(t),
	}
	r := runEdit(t, opts, terminalEnv(""))
	if r.err == nil {
		t.Fatal("Run succeeded, want the temporary directory error")
	}
	if !strings.HasPrefix(r.err.Error(), "config edit: ") {
		t.Errorf("error %q does not start with %q", r.err, "config edit: ")
	}
}

func TestEditFailsWhenTheFileCannotBeRead(t *testing.T) {
	opts := options(t, "")
	if err := os.Mkdir(opts.Path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	opts.Editor = neverCalled(t)
	r := runEdit(t, opts, terminalEnv(""))
	if r.err == nil {
		t.Fatal("Run succeeded, want the read error")
	}
	if errors.Is(r.err, fs.ErrNotExist) {
		t.Errorf("Run = %v, want an error other than the file not existing", r.err)
	}
	if !strings.HasPrefix(r.err.Error(), "config edit: ") {
		t.Errorf("error %q does not start with %q", r.err, "config edit: ")
	}
	if info, err := os.Stat(opts.Path); err != nil || !info.IsDir() {
		t.Errorf("Stat of %s = %v, %v, want the directory left in place", opts.Path, info, err)
	}
	assertNoTempDir(t, opts.Path)
}

// TestEditFailsWhenTheDefaultCannotBeSeeded edits a missing file of a configuration type that is
// not a struct, for which no default file can be written.
func TestEditFailsWhenTheDefaultCannotBeSeeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	called := false
	opts := config.Options[string]{
		Path:     path,
		Defaults: "tool",
		Editor:   func(context.Context, *spec.Env, string) error { called = true; return nil },
	}
	var errOut bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdin: strings.NewReader(""), Stderr: &errOut, Terminal: stdinTerminal}, nil, nil)
	err := subcommand(t, config.New(opts), "edit").Run(context.Background(), inv)
	if !errors.Is(err, cfg.ErrSchema) {
		t.Fatalf("Run = %v, want an error matching cfg.ErrSchema", err)
	}
	if called {
		t.Error("the editor ran, want it not to")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Stat = %v, want no file created", err)
	}
	assertNoTempDir(t, path)
}

func TestEditFailsWhenTheEditorRemovesTheCopy(t *testing.T) {
	const original = "name: tool\n"
	opts := options(t, original)
	opts.Editor = func(_ context.Context, _ *spec.Env, path string) error { return os.Remove(path) }
	r := runEdit(t, opts, terminalEnv(""))
	if !errors.Is(r.err, fs.ErrNotExist) {
		t.Fatalf("Run = %v, want an error matching fs.ErrNotExist", r.err)
	}
	if !strings.HasPrefix(r.err.Error(), "config edit: ") {
		t.Errorf("error %q does not start with %q", r.err, "config edit: ")
	}
	assertContent(t, opts.Path, original)
	assertNoTempDir(t, opts.Path)
}

// TestEditReportsAFailedReplace has the editor turn the configuration file into a non-empty
// directory, which a valid copy cannot be renamed over.
func TestEditReportsAFailedReplace(t *testing.T) {
	opts := options(t, "name: tool\n")
	opts.Editor = func(_ context.Context, _ *spec.Env, path string) error {
		if err := os.WriteFile(path, []byte(validEdit), 0o600); err != nil {
			return err
		}
		if err := os.Remove(opts.Path); err != nil {
			return err
		}
		if err := os.Mkdir(opts.Path, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(opts.Path, "inside"), []byte("x"), 0o600)
	}
	r := runEdit(t, opts, terminalEnv(""))
	if r.err == nil {
		t.Fatal("Run succeeded, want the replace error")
	}
	if want := "config edit: state: rename "; !strings.HasPrefix(r.err.Error(), want) {
		t.Errorf("error %q does not start with %q", r.err, want)
	}
	if strings.Contains(r.stderr, "updated") {
		t.Errorf("stderr = %q, want no update reported", r.stderr)
	}
	assertContent(t, filepath.Join(opts.Path, "inside"), "x")
	assertNoTempDir(t, opts.Path)
}

func TestEditReportsAPromptItCannotWrite(t *testing.T) {
	const original = "name: tool\n"
	opts, fake := editWith(t, original, invalidEdit)
	failure := errors.New("stderr closed")
	env := terminalEnv("y\n")
	env.Stderr = errWriter{err: failure}
	r := runEdit(t, opts, env)
	if !errors.Is(r.err, failure) {
		t.Fatalf("Run = %v, want the write error", r.err)
	}
	if errors.Is(r.err, config.ErrEditAbandoned) {
		t.Errorf("Run = %v, want the write error, not an abandoned edit", r.err)
	}
	if len(fake.seen) != 1 {
		t.Errorf("editor opened %d times, want once", len(fake.seen))
	}
	assertContent(t, opts.Path, original)
	assertNoTempDir(t, opts.Path)
}

func TestEditReportsAnAnswerItCannotRead(t *testing.T) {
	const original = "name: tool\n"
	opts, fake := editWith(t, original, invalidEdit)
	failure := errors.New("stdin broken")
	env := &spec.Env{Stdin: iotest.ErrReader(failure), Terminal: stdinTerminal}
	r := runEdit(t, opts, env)
	if !errors.Is(r.err, failure) {
		t.Fatalf("Run = %v, want the read error", r.err)
	}
	if errors.Is(r.err, config.ErrEditAbandoned) {
		t.Errorf("Run = %v, want the read error, not an abandoned edit", r.err)
	}
	if got := strings.Count(r.stderr, "re-edit? [Y/n]"); got != 1 {
		t.Errorf("prompted %d times, want once; stderr %q", got, r.stderr)
	}
	if len(fake.seen) != 1 {
		t.Errorf("editor opened %d times, want once", len(fake.seen))
	}
	assertContent(t, opts.Path, original)
	assertNoTempDir(t, opts.Path)
}

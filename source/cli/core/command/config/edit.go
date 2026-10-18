package config

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cfg "go.vipengele.org/config"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/state"
)

// ErrNoTerminal reports that "config edit" was run with a standard input that is not a terminal,
// so no editor could take the user's keystrokes and no prompt could be answered.
var ErrNoTerminal = errors.New("the standard input is not a terminal")

// ErrNoEditor reports that neither $VISUAL nor $EDITOR names an editor for "config edit".
var ErrNoEditor = errors.New("no editor configured: set $VISUAL or $EDITOR")

// ErrEditAbandoned reports that "config edit" ended on an invalid edit the user declined to edit
// again, so the configuration file was left as it was.
var ErrEditAbandoned = errors.New("edit abandoned")

// ErrNotFileStdio reports that the default editor was given a standard stream that is not an
// *os.File. exec.Cmd copies any other reader or writer through a pipe, and an editor reading a
// pipe has no terminal to draw on.
var ErrNotFileStdio = errors.New("the editor needs the standard streams to be files")

// editMode is the permission of the configuration file "config edit" writes.
const editMode = 0o600

// newEdit returns "config edit", which opens a copy of the file at opts.Path in an editor and
// moves it over opts.Path once the editor leaves it valid.
//
// The standard input must be a terminal, as [spec.Env.IsTerminal] reports it; otherwise the
// command returns an error matching [ErrNoTerminal] and creates nothing. The editor is
// opts.Editor, or with a nil opts.Editor the program $VISUAL names, else $EDITOR, as the
// invocation's Env looks them up; with neither the error matches [ErrNoEditor].
//
// The copy is a file with the base name of opts.Path, so that problems name "config.yaml:3", in a
// new directory ".edit-*" beside opts.Path. The directory of opts.Path is created with mode 0700
// when it is missing, and the temporary directory is removed however the command ends. The copy
// holds the bytes of the file, or the commented default [cfg.Seed] writes when there is no file.
//
// Once the editor returns, the copy is checked as "config validate" checks the file: the defaults
// under the copy, parsed strictly, and the result checked by T's Validator; the environment is not
// read. A copy whose bytes equal what it started with is no change: the command writes "no
// changes" on stderr and leaves opts.Path as it is, which for a missing file means no file is
// created. A valid changed copy is given mode 0600 and renamed over opts.Path. An invalid copy has
// its problems written on stderr, followed by the prompt "re-edit? [Y/n]" answered by a line of
// the standard input: an empty answer, "y" or "yes" opens the same copy again, and "n", "no" or
// the end of the input declines, returning an error matching [ErrEditAbandoned] with opts.Path
// untouched. Any other answer asks again.
func newEdit[T any](opts Options[T]) *spec.Command {
	return &spec.Command{
		Name:        "edit",
		Summary:     "Edit the configuration file",
		Args:        spec.NoArgs(),
		Annotations: skipConfig(),
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			if !inv.Env.IsTerminal(spec.Stdin) {
				return fmt.Errorf("config edit: %w", ErrNoTerminal)
			}
			editor := opts.Editor
			if editor == nil {
				argv, err := editorCommand(inv.Env.LookupEnv)
				if err != nil {
					return fmt.Errorf("config edit: %w", err)
				}
				editor = launcher(argv)
			}
			return edit(ctx, inv, opts, editor)
		},
	}
}

// edit runs "config edit" for opts with editor, once the terminal and the editor are settled.
func edit[T any](ctx context.Context, inv *spec.Invocation, opts Options[T], editor Editor) error {
	dir := filepath.Dir(opts.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config edit: %w", err)
	}
	tempDir, err := os.MkdirTemp(dir, ".edit-*")
	if err != nil {
		return fmt.Errorf("config edit: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	temp := filepath.Join(tempDir, filepath.Base(opts.Path))
	start, err := prepareCopy(opts, temp)
	if err != nil {
		return fmt.Errorf("config edit: %w", err)
	}

	answers := bufio.NewReader(inv.Env.Stdin)
	for {
		if err := editor(ctx, inv.Env, temp); err != nil {
			return fmt.Errorf("config edit: editor: %w", err)
		}
		edited, err := os.ReadFile(filepath.Clean(temp))
		if err != nil {
			return fmt.Errorf("config edit: %w", err)
		}
		if bytes.Equal(edited, start) {
			inv.Success("no changes: %s is unchanged", opts.Path)
			return nil
		}
		_, loadErr := cfg.Load(opts.Defaults, cfg.File(temp))
		if loadErr == nil {
			if err := state.Replace(temp, opts.Path, editMode); err != nil {
				return fmt.Errorf("config edit: %w", err)
			}
			inv.Success("%s updated", opts.Path)
			return nil
		}
		inv.Error("%s", loadErr)
		again, err := askReEdit(inv.Env.Stderr, answers)
		if err != nil {
			return fmt.Errorf("config edit: %w", err)
		}
		if !again {
			return fmt.Errorf("config edit: %w: %s unchanged", ErrEditAbandoned, opts.Path)
		}
	}
}

// prepareCopy writes the copy the editor opens at temp, which does not exist yet: the bytes of the
// file at opts.Path, or the default [cfg.Seed] writes when there is none. It returns the copy's
// content, against which the edited copy is compared.
func prepareCopy[T any](opts Options[T], temp string) ([]byte, error) {
	data, err := os.ReadFile(opts.Path)
	switch {
	case err == nil:
		return data, state.WriteFile(temp, data, editMode)
	case errors.Is(err, fs.ErrNotExist):
		if _, err := cfg.Seed(temp, opts.Defaults); err != nil {
			return nil, err
		}
		return os.ReadFile(filepath.Clean(temp))
	default:
		return nil, err
	}
}

// askReEdit writes the prompt "re-edit? [Y/n] " to w and reads answers from r until one is
// recognised: true for an empty line, "y" or "yes", false for "n", "no" or the end of r, in any
// case and with surrounding whitespace ignored. r is the one reader of the standard input for the
// whole run, so that a line it has buffered is not lost to the next prompt.
func askReEdit(w io.Writer, r *bufio.Reader) (bool, error) {
	for {
		if _, err := io.WriteString(w, "re-edit? [Y/n] "); err != nil {
			return false, err // [lydite:exclude_from_mutation][edit returns the error without reading the answer, so the bool returned with it is unobservable]
		}
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err // [lydite:exclude_from_mutation][edit returns the error without reading the answer, so the bool returned with it is unobservable]
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		switch {
		case errors.Is(err, io.EOF) && line == "":
			return false, nil
		case answer == "" || answer == "y" || answer == "yes":
			return true, nil
		case answer == "n" || answer == "no":
			return false, nil
		case errors.Is(err, io.EOF):
			return false, nil
		}
	}
}

// editorCommand returns the program and arguments of the editor $VISUAL names, or else $EDITOR,
// split on whitespace so that "code -w" is the program "code" with the argument "-w". A variable
// that is unset or holds only whitespace names no editor; with neither, the error is [ErrNoEditor].
func editorCommand(lookup func(string) (string, bool)) ([]string, error) {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if value, ok := lookup(name); ok {
			if argv := strings.Fields(value); len(argv) > 0 {
				return argv, nil
			}
		}
	}
	return nil, ErrNoEditor
}

// launcher returns the Editor that runs argv with the file's path as its last argument, in the
// working directory of env, and waits for it to exit. The process inherits the environment of
// this process. Each of env's standard streams must be an *os.File, handed to the editor as it
// is so that the editor reaches the terminal; otherwise the Editor returns an error matching
// [ErrNotFileStdio] and runs nothing. An exit status other than 0 is an error.
func launcher(argv []string) Editor {
	return func(ctx context.Context, env *spec.Env, path string) error {
		stdin, inOK := env.Stdin.(*os.File)
		stdout, outOK := env.Stdout.(*os.File)
		stderr, errOK := env.Stderr.(*os.File)
		if !inOK || !outOK || !errOK || stdin == nil || stdout == nil || stderr == nil {
			return ErrNotFileStdio
		}
		args := append(argv[1:len(argv):len(argv)], path)
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- argv is the editor the invoking user named in $VISUAL or $EDITOR, run without a shell, with the file path as its own argument
		cmd := exec.CommandContext(ctx, argv[0], args...) // #nosec G204 -- argv is the editor the invoking user named in $VISUAL or $EDITOR, run without a shell, with the file path as its own argument; launching it is the purpose of config edit
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
		cmd.Dir = env.WorkDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", argv[0], err)
		}
		return nil
	}
}

package cli_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// detailErr is an error a renderer prints with detail rows.
type detailErr struct {
	field string
	notes []string
}

func (e detailErr) Error() string { return "invalid " + e.field }

// updateGolden reports whether the -update flag TestMain registers is set.
func updateGolden() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// checkGolden compares got with testdata/render/<name>.golden, which -update rewrites. A missing
// file fails the test.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "render", name+".golden")
	if updateGolden() {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the golden file (run with -update to create it): %v", err)
	}
	if string(want) != got {
		t.Errorf("stderr differs from %s\n got: %q\nwant: %q", path, got, string(want))
	}
}

func TestRenderGolden(t *testing.T) {
	openErr := errors.New("file does not exist")
	tests := []struct {
		name string
		app  func() *cli.App
		args []string
		code int
	}{
		{
			name: "flat",
			app:  func() *cli.App { return exitApp(errors.New("boom")) },
			args: []string{"do"},
			code: cli.ExitError,
		},
		{
			name: "wrapped",
			app: func() *cli.App {
				return exitApp(fmt.Errorf("load config: %w",
					fmt.Errorf("open app.yml: %w", openErr)))
			},
			args: []string{"do"},
			code: cli.ExitError,
		},
		{
			name: "joined",
			app: func() *cli.App {
				return exitApp(errors.Join(errors.New("name: required"), errors.New("port: out of range")))
			},
			args: []string{"do"},
			code: cli.ExitError,
		},
		{
			name: "wrap-of-join",
			app: func() *cli.App {
				return exitApp(fmt.Errorf("load config: %w",
					fmt.Errorf("validate tool.yaml: %w",
						errors.Join(errors.New("name: required"),
							fmt.Errorf("port: %w", errors.New("out of range"))))))
			},
			args: []string{"do"},
			code: cli.ExitError,
		},
		{
			name: "typed",
			app: func() *cli.App {
				err := fmt.Errorf("load config: %w", detailErr{field: "port", notes: []string{"must be 1-65535", "got 70000"}})
				return exitApp(err, cli.WithErrorRenderer(func(e detailErr) string {
					out := "invalid " + e.field
					for _, n := range e.notes {
						out += "\n" + n
					}
					return out
				}))
			},
			args: []string{"do"},
			code: cli.ExitError,
		},
		{
			name: "invalid-tree",
			app: func() *cli.App {
				run := func(context.Context, *spec.Invocation) error { return nil }
				app := cli.New(identity())
				app.Add(&spec.Command{Name: "dup", Run: run})
				app.Add(&spec.Command{Name: "dup", Run: run})
				app.Add(&spec.Command{Name: "empty"})
				return app
			},
			args: []string{"dup"},
			code: cli.ExitError,
		},
		{
			name: "usage",
			app:  func() *cli.App { return exitApp(nil) },
			args: []string{"do", "--nope"},
			code: cli.ExitUsage,
		},
		{
			name: "run-usage",
			app: func() *cli.App {
				return exitApp(&spec.UsageError{Question: "delete 3 files?", Flag: "--yes"})
			},
			args: []string{"do"},
			code: cli.ExitUsage,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			env := &spec.Env{Stdout: &stdout, Stderr: &stderr, WorkDir: t.TempDir()}
			if code := tt.app().ExecuteArgs(env, tt.args); code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
			}
			checkGolden(t, tt.name, stderr.String())
		})
	}
}

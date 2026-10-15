package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/terminal"
)

// The exit codes [Execute] and [ExecuteArgs] return.
const (
	// ExitOK is the exit code of a run function that returns nil, and of --help.
	ExitOK = 0
	// ExitError is the exit code of a run function that returns an error, and of a tree that
	// [spec.Command.Validate] rejects.
	ExitError = 1
	// ExitUsage is the exit code of a command line the tree does not accept: an unknown flag, a
	// flag value that does not parse, an unknown subcommand, or a number of positional arguments
	// the command's [spec.ArgPolicy] rejects.
	ExitUsage = 2
)

// Execute runs root with the arguments of the process, os.Args[1:], against the environment of
// the process: its standard streams, working directory and environment variables. It returns
// the exit code, which a tool's main passes to os.Exit:
//
//	func main() {
//		os.Exit(cli.Execute(newRoot()))
//	}
//
// It behaves as [ExecuteArgs] does, and returns [ExitError] after writing "<name>: <error>" to
// os.Stderr when the working directory cannot be determined.
func Execute(root *spec.Command) int {
	return executeProcess(prefix(root), func(env *spec.Env, args []string) int {
		return ExecuteArgs(env, root, args)
	})
}

// executeProcess calls run with the environment and the arguments of the process, and returns
// its exit code. It returns [ExitError] without calling run after writing the error, preceded
// by errPrefix, to os.Stderr when the working directory cannot be determined.
func executeProcess(errPrefix string, run func(env *spec.Env, args []string) int) int {
	env, err := processEnv()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s%v\n", errPrefix, err)
		return ExitError
	}
	return run(env, os.Args[1:])
}

// processEnv returns the environment of the process.
func processEnv() (*spec.Env, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &spec.Env{
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		WorkDir:   wd,
		LookupEnv: os.LookupEnv,
	}, nil
}

// ExecuteArgs runs root with args against env, and returns the exit code. It touches no part of
// the process that env does not give it: a nil env, or a nil field of env, holds the inert
// default [spec.Env.Resolved] gives it, and a nil args is no arguments. Every run function it
// calls receives context.Background().
//
// It checks root with [spec.Command.Validate] on every call, and returns [ExitError] after
// writing "<name>: invalid command tree: <error>" to env.Stderr when the tree is invalid. It
// then selects the command args name and calls its run function. Every error is written to
// env.Stderr as "<name>: <error>", where name is root's Name, and the exit code is [ExitUsage]
// for a command line the tree does not accept and [ExitError] for an error the run function
// returns. --help, on any command, writes the command's help to env.Stdout and returns
// [ExitOK]. A command with subcommands and no Run, given no positional argument, does the same.
// There is no help or completion subcommand.
//
// A nil env.Terminal is filled with [terminal.Detect] of the resolved env's Stdin, Stdout and
// Stderr, so [spec.Env.IsTerminal] answers for a real file instead of panicking; a non-nil
// env.Terminal is used as is. The Terminal is set on the resolved copy, and env is not modified.
//
// Every run function receives a [spec.Invocation] whose Output forbids colour when the
// environment variable NO_COLOR is set and not empty, or when env.Stdout is not a terminal.
//
// One tree may be run any number of times, concurrently: each call parses its own flag values.
func ExecuteArgs(env *spec.Env, root *spec.Command, args []string) int {
	return executeArgs(env, root, args, cobra.Config{Output: outputResolver{}.resolve})
}

// executeArgs is [ExecuteArgs] with the adapter configured by cfg.
func executeArgs(env *spec.Env, root *spec.Command, args []string, cfg cobra.Config) int {
	env = env.Resolved()
	if env.Terminal == nil {
		env.Terminal = terminal.Detect(env.Stdin, env.Stdout, env.Stderr)
	}
	if err := root.Validate(); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "%sinvalid command tree: %v\n", prefix(root), err)
		return ExitError
	}
	err := cobra.Run(context.Background(), env, root, args, cfg)
	if err == nil {
		return ExitOK
	}
	_, _ = fmt.Fprintf(env.Stderr, "%s%v\n", prefix(root), err)
	var usage *cobra.UsageError
	if errors.As(err, &usage) {
		return ExitUsage
	}
	return ExitError
}

// prefix returns "<name>: " for root's name, and "" for a nil root or an empty name.
func prefix(root *spec.Command) string {
	if root == nil || root.Name == "" {
		return ""
	}
	return root.Name + ": "
}
